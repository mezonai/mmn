package ledger

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mezonai/mmn/store"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/types"
)

var (
	ErrAccountExisted = errors.New("account existed")
)

type Ledger struct {
	mu           sync.RWMutex
	txStore      store.TxStore
	txMetaStore  store.TxMetaStore
	accountStore store.AccountStore
	eventRouter  *events.EventRouter
}

func NewLedger(txStore store.TxStore, txMetaStore store.TxMetaStore, accountStore store.AccountStore, eventRouter *events.EventRouter) *Ledger {
	return &Ledger{
		txStore:      txStore,
		txMetaStore:  txMetaStore,
		accountStore: accountStore,
		eventRouter:  eventRouter,
	}
}

// CreateAccount creates and stores a new account into db, return error if an account with the same addr existed
func (l *Ledger) CreateAccount(addr string, balance uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	_, err := l.createAccountWithoutLocking(addr, balance)
	return err
}

// createAccountWithoutLocking creates account and store in db without locking ledger. This is useful
// when calling method has already acquired lock for ledger to avoid recursive locking and deadlock
func (l *Ledger) createAccountWithoutLocking(addr string, balance uint64) (*types.Account, error) {
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
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.accountStore.ExistsByAddr(addr)
}

// Balance returns current balance for addr
func (l *Ledger) Balance(addr string) (uint64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	acc, err := l.accountStore.GetByAddr(addr)
	if err != nil {
		return 0, err
	}

	return acc.Balance, nil
}

func (l *Ledger) ApplyBlock(b *block.Block) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Printf("[ledger] Applying block %d\n", b.Slot)

	for _, entry := range b.Entries {
		txs, err := l.txStore.GetBatch(entry.TxHashes)
		if err != nil {
			return err
		}
		txMetas := make([]*types.TransactionMeta, 0, len(txs))

		for _, tx := range txs {
			// load account state
			sender, err := l.accountStore.GetByAddr(tx.Sender)
			if err != nil {
				return err
			}
			if sender == nil {
				if sender, err = l.createAccountWithoutLocking(tx.Sender, 0); err != nil {
					return err
				}
			}
			recipient, err := l.accountStore.GetByAddr(tx.Recipient)
			if err != nil {
				return err
			}
			if recipient == nil {
				if recipient, err = l.createAccountWithoutLocking(tx.Recipient, 0); err != nil {
					return err
				}
			}
			state := map[string]*types.Account{
				sender.Address:    sender,
				recipient.Address: recipient,
			}

			// try to apply tx
			if err := applyTx(state, tx); err != nil {
				// Publish specific transaction failure event
				if l.eventRouter != nil {
					txHash := tx.Hash()
					event := events.NewTransactionFailed(txHash, fmt.Sprintf("transaction application failed: %v", err))
					l.eventRouter.PublishTransactionEvent(event)
				}
				fmt.Printf("Apply fail: %v\n", err)
				state[tx.Sender].Nonce++
				txMetas = append(txMetas, types.NewTxMeta(tx, b.Slot, hex.EncodeToString(b.Hash[:]), types.TxStatusFailed, err.Error()))
				continue
			}
			fmt.Printf("Applied tx %s\n", tx.Hash())
			txMetas = append(txMetas, types.NewTxMeta(tx, b.Slot, hex.EncodeToString(b.Hash[:]), types.TxStatusSuccess, ""))
			addHistory(state[tx.Sender], tx)
			if tx.Recipient != tx.Sender {
				addHistory(state[tx.Recipient], tx)
			}

			// commit the update
			if err := l.accountStore.StoreBatch([]*types.Account{sender, recipient}); err != nil {
				return err
			}
		}
		if len(txMetas) > 0 {
			l.txMetaStore.StoreBatch(txMetas)
		}
	}

	fmt.Printf("[ledger] Block %d applied\n", b.Slot)
	return nil
}

// GetAccount returns account with addr (nil if not exist)
func (l *Ledger) GetAccount(addr string) (*types.Account, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.accountStore.GetByAddr(addr)
}

// Apply transaction to ledger (after verifying signature). NOTE: this does not perform persisting operation into db
func applyTx(state map[string]*types.Account, tx *transaction.Transaction) error {
	sender, ok := state[tx.Sender]
	if !ok {
		state[tx.Sender] = &types.Account{Address: tx.Sender, Balance: 0, Nonce: 0}
		sender = state[tx.Sender]
	}
	recipient, ok := state[tx.Recipient]
	if !ok {
		state[tx.Recipient] = &types.Account{Address: tx.Recipient, Balance: 0, Nonce: 0}
		recipient = state[tx.Recipient]
	}

	if sender.Balance < tx.Amount {
		return fmt.Errorf("insufficient balance")
	}
	// Strict nonce validation to prevent duplicate transactions
	if tx.Nonce != sender.Nonce+1 {
		return fmt.Errorf("invalid nonce: expected %d, got %d", sender.Nonce+1, tx.Nonce)
	}
	sender.Balance -= tx.Amount
	recipient.Balance += tx.Amount
	sender.Nonce = tx.Nonce
	return nil
}

func addHistory(acc *types.Account, tx *transaction.Transaction) {
	acc.History = append(acc.History, tx.Hash())
}

func (l *Ledger) GetTxByHash(hash string) (*transaction.Transaction, *types.TransactionMeta, error) {
	tx, err := l.txStore.GetByHash(hash)
	txMeta, err := l.txMetaStore.GetByHash(hash)
	if err != nil {
		return nil, nil, err
	}
	return tx, txMeta, nil
}

func (l *Ledger) GetTxs(addr string, limit uint32, offset uint32, filter uint32) (uint32, []*transaction.Transaction) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	txs := make([]*transaction.Transaction, 0)
	acc, err := l.accountStore.GetByAddr(addr)
	if err != nil {
		return 0, txs
	}

	// filter type: 0: all, 1: sender, 2: recipient
	filteredHistory := make([]*transaction.Transaction, 0)
	transactions, err := l.txStore.GetBatch(acc.History)
	if err != nil {
		return 0, txs
	}
	for _, tx := range transactions {
		if filter == 0 {
			filteredHistory = append(filteredHistory, tx)
		} else if filter == 1 && tx.Sender == addr {
			filteredHistory = append(filteredHistory, tx)
		} else if filter == 2 && tx.Recipient == addr {
			filteredHistory = append(filteredHistory, tx)
		}
	}

	total := uint32(len(filteredHistory))
	start := offset
	end := min(start+limit, total)
	txs = filteredHistory[start:end]

	return total, txs
}
