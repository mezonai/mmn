package ledger

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/monitoring"
	"github.com/mezonai/mmn/security/validation"
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

	txHashes := make([]string, 0)
	txMetas := make(map[string]*types.TransactionMeta)
	accAddrs := make([]string, 0)
	accAddrsSet := make(map[string]struct{})
	parentContentHashes := make([]string, 0)
	rootContentHashes := make(map[string]struct{})
	txContents := make([]*transaction.Transaction, 0)

	for _, entry := range b.Entries {
		if entry.Tick {
			continue
		}
		txHashes = append(txHashes, entry.TxHashes...)
	}

	allTxsInBlock, err := l.txStore.GetBatch(txHashes)
	if err != nil {
		return fmt.Errorf("failed to get transactions for block %d: %w", b.Slot, err)
	}

	for _, tx := range allTxsInBlock {
		if tx.Type == transaction.TxTypeUserContent {
			continue
		}
		if _, exists := accAddrsSet[tx.Sender]; !exists {
			accAddrsSet[tx.Sender] = struct{}{}
			accAddrs = append(accAddrs, tx.Sender)
		}
		if _, exists := accAddrsSet[tx.Recipient]; !exists {
			accAddrsSet[tx.Recipient] = struct{}{}
			accAddrs = append(accAddrs, tx.Recipient)
		}
	}

	state, err := l.accountStore.GetBatch(accAddrs)
	if err != nil {
		return fmt.Errorf("failed to get accounts for block %d: %w", b.Slot, err)
	}

	for _, tx := range allTxsInBlock {
		txHash := tx.Hash()

		if tx.Type == transaction.TxTypeUserContent {
			var content types.UserContent
			if err = json.Unmarshal([]byte(tx.ExtraInfo), &content); err != nil {
				txMetas[txHash] = types.NewTxMeta(tx, b.Slot, hex.EncodeToString(b.Hash[:]), types.TxStatusFailed, "User content data is invalid")
				continue
			}

			txContents = append(txContents, tx)
			if content.ParentHash != "" {
				parentContentHashes = append(parentContentHashes, content.ParentHash)
			}
			if content.RootHash != "" {
				rootContentHashes[content.RootHash] = struct{}{}
			}
		} else {
			if err = applyTx(state, tx); err != nil {
				logx.Warn("LEDGER", fmt.Sprintf("Apply fail: %v", err))
				txMetas[txHash] = types.NewTxMeta(tx, b.Slot, hex.EncodeToString(b.Hash[:]), types.TxStatusFailed, err.Error())
			} else {
				logx.Debug("LEDGER", fmt.Sprintf("Applied tx %s", txHash))
				txMetas[txHash] = types.NewTxMeta(tx, b.Slot, hex.EncodeToString(b.Hash[:]), types.TxStatusSuccess, "")
			}
		}

		// Remove transaction from tracker
		if l.txTracker != nil && !isListener {
			l.txTracker.RemoveTransaction(txHash)
		}
	}

	parentContentTxs, err := l.txStore.GetBatch(parentContentHashes)
	if err != nil {
		return fmt.Errorf("failed to get parent transaction contents for block %d: %w", b.Slot, err)
	}
	parentContentTxMap := make(map[string]*transaction.Transaction)
	for _, tx := range parentContentTxs {
		txHash := tx.Hash()
		parentContentTxMap[txHash] = tx
	}

	latestVersionContentHashMap, err := l.txStore.GetBatchLatestVersionContentHash(rootContentHashes)
	if err != nil {
		return fmt.Errorf("failed to get latest version content hashes for block %d: %w", b.Slot, err)
	}

	for _, content := range txContents {
		if err := l.validateUserContent(content, parentContentTxMap, latestVersionContentHashMap); err != nil {
			txMetas[content.Hash()] = types.NewTxMeta(content, b.Slot, hex.EncodeToString(b.Hash[:]), types.TxStatusFailed, err.Error())
			continue
		}
		txMetas[content.Hash()] = types.NewTxMeta(content, b.Slot, hex.EncodeToString(b.Hash[:]), types.TxStatusSuccess, "")
	}

	if err := l.bStore.FinalizeBlock(b, txMetas, state, latestVersionContentHashMap); err != nil {
		if l.eventRouter != nil {
			for _, tx := range allTxsInBlock {
				event := events.NewTransactionFailed(tx, fmt.Sprintf("WAL write failed for block %d: %v", b.Slot, err))
				l.eventRouter.PublishTransactionEvent(event)
				monitoring.IncreaseFailedTpsCount(err.Error())
			}
		}
		return fmt.Errorf("finalized block error: %w", err)
	}

	if l.eventRouter != nil {
		blockHashHex := b.HashString()
		now := time.Now()
		for _, tx := range allTxsInBlock {
			if meta, exists := txMetas[tx.Hash()]; exists && meta.Status == types.TxStatusFailed {
				// Publish specific transaction failure event
				event := events.NewTransactionFailed(tx, fmt.Sprintf("transaction application failed: %v", meta.Error))
				l.eventRouter.PublishTransactionEvent(event)
				monitoring.IncreaseFailedTpsCount(meta.Error)
				continue
			}

			// Record metrics
			txTimestamp := time.UnixMilli(int64(tx.Timestamp))
			monitoring.RecordTimeToFinality(now.Sub(txTimestamp))

			event := events.NewTransactionFinalized(tx, b.Slot, blockHashHex)
			l.eventRouter.PublishTransactionEvent(event)
			monitoring.IncreaseFinalizedTpsCount()
		}
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

// Validate transaction content
func (l *Ledger) validateUserContent(tx *transaction.Transaction, parentContentTxMap map[string]*transaction.Transaction, latestVersionContentHashMap map[string]string) error {
	var content types.UserContent
	_ = json.Unmarshal([]byte(tx.ExtraInfo), &content)

	if strings.TrimSpace(content.Type) == "" ||
		strings.TrimSpace(content.Title) == "" ||
		strings.TrimSpace(content.Description) == "" {
		return fmt.Errorf("user content required fields are missing")
	}

	if validation.ShouldValidateAddress(content.Type) {
		if !validation.ValidateTxAddress(tx.Recipient) {
			return fmt.Errorf("transaction address is invalid")
		}
	}

	if content.ParentHash == "" && content.RootHash == "" {
		latestVersionContentHashMap[tx.Hash()] = tx.Hash()
		return nil
	}

	if content.ParentHash == "" || content.RootHash == "" {
		return fmt.Errorf("user content data is invalid")
	}

	parentContentTx, exists := parentContentTxMap[content.ParentHash]
	if !exists {
		return fmt.Errorf("parent user content not found")
	}

	if parentContentTx.Type != transaction.TxTypeUserContent ||
		parentContentTx.Sender != tx.Sender ||
		parentContentTx.Recipient != tx.Recipient {
		return fmt.Errorf("user content data is invalid")
	}

	latestVersionContentHash, exists := latestVersionContentHashMap[content.RootHash]
	if !exists {
		return fmt.Errorf("latest version user content not found")
	}

	if latestVersionContentHash != content.ParentHash {
		return fmt.Errorf("parent user content is not the latest version")
	}

	latestVersionContentHashMap[content.RootHash] = tx.Hash()
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

var ErrInvalidNonce = errors.New("invalid nonce")
