package ledger

import (
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/monitoring"
	"github.com/mezonai/mmn/store"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/types"
)

var (
	ErrAccountExisted = errors.New("account existed")
)

type Ledger struct {
	mu           sync.RWMutex
	bStore       store.BlockStore
	txStore      store.TxStore
	txMetaStore  store.TxMetaStore
	accountStore store.AccountStore
	eventRouter  *events.EventRouter
	txTracker    interfaces.TransactionTrackerInterface
}

func NewLedger(bStore store.BlockStore, txStore store.TxStore, txMetaStore store.TxMetaStore, accountStore store.AccountStore, eventRouter *events.EventRouter, txTracker interfaces.TransactionTrackerInterface) *Ledger {
	return &Ledger{
		bStore:       bStore,
		txStore:      txStore,
		txMetaStore:  txMetaStore,
		accountStore: accountStore,
		eventRouter:  eventRouter,
		txTracker:    txTracker,
	}
}

// createAccountWithoutLocking creates account and store in db without locking ledger.
func (l *Ledger) createAccountWithoutLocking(addr string, balance *uint256.Int) (*types.Account, error) {
	existed, err := l.accountStore.ExistsByAddr(addr)
	if err != nil {
		return nil, fmt.Errorf("could not check existence of account: %w", err)
	}
	if existed {
		return nil, ErrAccountExisted
	}

	account := &types.Account{
		Address: addr,
		Balance: balance,
		Nonce:   0,
	}
	err = l.accountStore.Store(account)
	if err != nil {
		return nil, fmt.Errorf("failed to store account: %w", err)
	}

	return account, nil
}

// CreateAccountsFromGenesis creates an account from genesis block (implements LedgerInterface)
func (l *Ledger) CreateAccountsFromGenesis(addrs []config.Address) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, addr := range addrs {
		_, err := l.createAccountWithoutLocking(addr.Address, addr.Amount)
		if err != nil {
			return fmt.Errorf("could not create genesis account %s: %w", addr.Address, err)
		}
	}
	return nil
}

// AccountExists checks if an account exists (implements LedgerInterface)
func (l *Ledger) AccountExists(addr string) (bool, error) {
	return l.accountStore.ExistsByAddr(addr)
}

// Balance returns current balance for addr
func (l *Ledger) Balance(addr string) (*uint256.Int, error) {
	acc, err := l.accountStore.GetByAddr(addr)
	if err != nil {
		return uint256.NewInt(0), err
	}

	return acc.Balance, nil
}

func (l *Ledger) FinalizeBlock(b *block.Block, isListener bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	logx.Info("LEDGER", fmt.Sprintf("Applying block %d", b.Slot))
	if b.InvalidPoH {
		logx.Warn("LEDGER", fmt.Sprintf("Block %d processed as InvalidPoH", b.Slot))
		return nil
	}

	successfulTxs := make([]*transaction.Transaction, 0)
	txMetas := make([]*types.TransactionMeta, 0)
	state := map[string]*types.Account{}

	for _, entry := range b.Entries {
		if entry.Tick {
			continue
		}

		txs, err := l.txStore.GetBatch(entry.TxHashes)
		if err != nil {
			return err
		}

		for _, tx := range txs {
			if state[tx.Sender] == nil {
				sender, err := l.getAccountOrCreate(tx.Sender)
				if err != nil {
					return err
				}
				state[tx.Sender] = sender
			}
			if state[tx.Recipient] == nil {
				recipient, err := l.getAccountOrCreate(tx.Recipient)
				if err != nil {
					return err
				}
				state[tx.Recipient] = recipient
			}

			// try to apply tx
			txHash := tx.Hash()
			if err := applyTx(state, tx); err != nil {
				// Publish specific transaction failure event
				if l.eventRouter != nil {
					event := events.NewTransactionFailed(tx, fmt.Sprintf("transaction application failed: %v", err))
					l.eventRouter.PublishTransactionEvent(event)
					monitoring.IncreaseFailedTpsCount(err.Error())
				}
				logx.Warn("LEDGER", fmt.Sprintf("Apply fail: %v", err))
				txMetas = append(txMetas, types.NewTxMeta(tx, b.Slot, hex.EncodeToString(b.Hash[:]), types.TxStatusFailed, err.Error()))
				// Remove failed transaction from tracker
				if l.txTracker != nil && !isListener {
					l.txTracker.RemoveTransaction(txHash)
				}
				continue
			}
			logx.Debug("LEDGER", fmt.Sprintf("Applied tx %s", txHash))
			successfulTxs = append(successfulTxs, tx)
			txMetas = append(txMetas, types.NewTxMeta(tx, b.Slot, hex.EncodeToString(b.Hash[:]), types.TxStatusSuccess, ""))
			// Remove successful transaction from tracker
			if l.txTracker != nil && !isListener {
				l.txTracker.RemoveTransaction(txHash)
			}
		}
	}

	if err := l.bStore.FinalizeBlock(b.Slot, txMetas, state); err != nil {
		if l.eventRouter != nil {
			for _, tx := range successfulTxs {
				event := events.NewTransactionFailed(tx, fmt.Sprintf("WAL write failed for block %d: %v", b.Slot, err))
				l.eventRouter.PublishTransactionEvent(event)
				monitoring.IncreaseFailedTpsCount(err.Error())
			}
		}
		return fmt.Errorf("finalized block error: %w", err)
	}

	logx.Info("LEDGER", fmt.Sprintf("Block %d applied", b.Slot))
	return nil
}

// GetAccount returns account with addr (nil if not exist)
func (l *Ledger) GetAccount(addr string) (*types.Account, error) {
	return l.accountStore.GetByAddr(addr)
}

// GetAccountBatch returns multiple accounts for the given addresses using batch operation
func (l *Ledger) GetAccountBatch(addrs []string) (map[string]*types.Account, error) {
	return l.accountStore.GetBatch(addrs)
}

// Apply transaction to ledger (after verifying signature). NOTE: this does not perform persisting operation into db
func applyTx(state map[string]*types.Account, tx *transaction.Transaction) error {
	if tx == nil {
		return fmt.Errorf("transaction cannot be nil")
	}
	if tx.Amount == nil {
		return fmt.Errorf("transaction amount cannot be nil")
	}
	sender, ok := state[tx.Sender]
	if !ok {
		state[tx.Sender] = &types.Account{Address: tx.Sender, Balance: uint256.NewInt(0), Nonce: 0}
		sender = state[tx.Sender]
	}
	recipient, ok := state[tx.Recipient]
	if !ok {
		state[tx.Recipient] = &types.Account{Address: tx.Recipient, Balance: uint256.NewInt(0), Nonce: 0}
		recipient = state[tx.Recipient]
	}

	if sender.Balance.Cmp(tx.Amount) < 0 {
		return fmt.Errorf("insufficient balance")
	}

	sender.Balance.Sub(sender.Balance, tx.Amount)
	recipient.Balance.Add(recipient.Balance, tx.Amount)
	sender.Nonce = tx.Nonce
	return nil
}

func (l *Ledger) GetTxByHash(hash string) (*transaction.Transaction, *types.TransactionMeta, error, error) {
	tx, errTx := l.txStore.GetByHash(hash)
	txMeta, errTxMeta := l.txMetaStore.GetByHash(hash)
	if errTx != nil || errTxMeta != nil {
		return nil, nil, errTx, errTxMeta
	}
	return tx, txMeta, nil, nil
}

func (l *Ledger) GetTxBatch(hashes []string) ([]*transaction.Transaction, map[string]*types.TransactionMeta, error) {
	if len(hashes) == 0 {
		return []*transaction.Transaction{}, map[string]*types.TransactionMeta{}, nil
	}

	// Use batch operations - only 2 CGO calls instead of 2*N!
	txs, errTx := l.txStore.GetBatch(hashes)
	txMetas, errTxMeta := l.txMetaStore.GetBatch(hashes)

	if errTx != nil {
		return nil, nil, fmt.Errorf("failed to batch get transactions: %w", errTx)
	}
	if errTxMeta != nil {
		return nil, nil, fmt.Errorf("failed to batch get transaction metas: %w", errTxMeta)
	}

	return txs, txMetas, nil
}

func (l *Ledger) getAccountOrCreate(accAddr string) (*types.Account, error) {
	sender, err := l.accountStore.GetByAddr(accAddr)
	if err != nil {
		return nil, err
	}
	if sender == nil {
		if sender, err = l.createAccountWithoutLocking(accAddr, uint256.NewInt(0)); err != nil {
			return nil, err
		}
	}
	return sender, nil
}

var ErrInvalidNonce = errors.New("invalid nonce")
