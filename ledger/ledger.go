package ledger

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mezonai/mmn/blockstore"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/types"
	"github.com/mezonai/mmn/utils"
)

type Ledger struct {
	state   map[string]*types.Account // address (public key hex) → account
	mu      sync.RWMutex
	txStore blockstore.TxStore
}

func NewLedger(txStore blockstore.TxStore) *Ledger {
	return &Ledger{state: make(map[string]*types.Account), txStore: txStore}
}

// Initialize initial account
func (l *Ledger) CreateAccount(addr string, balance uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.state[addr] = &types.Account{Balance: balance, Nonce: 0}
}

// CreateAccountsFromGenesis creates an account from genesis block (implements LedgerInterface)
func (l *Ledger) CreateAccountsFromGenesis(addrs []config.Address) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, addr := range addrs {
		if _, exists := l.state[addr.Address]; exists {
			return fmt.Errorf("genesis account %s already exists", addr.Address)
		}

		l.state[addr.Address] = &types.Account{Balance: addr.Amount, Nonce: 0}
	}
	return nil
}

// AccountExists checks if an account exists (implements LedgerInterface)
func (l *Ledger) AccountExists(addr string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	_, exists := l.state[addr]
	return exists
}

// Query balance
func (l *Ledger) Balance(addr string) uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	acc, ok := l.state[addr]
	if !ok {
		return 0
	}
	return acc.Balance
}

func (l *Ledger) VerifyBlock(b *block.BroadcastedBlock) error {
	l.mu.RLock()
	base := l.state
	l.mu.RUnlock()

	view := &LedgerView{
		base:    base,
		overlay: make(map[string]*types.SnapshotAccount),
	}
	for _, entry := range b.Entries {
		for _, tx := range entry.Transactions {
			if err := view.ApplyTx(tx); err != nil {
				return fmt.Errorf("verify fail: %v", err)
			}
		}
	}
	return nil
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

		for _, tx := range txs {
			if err := applyTx(l.state, tx); err != nil {
				return fmt.Errorf("apply fail: %v", err)
			}
			fmt.Printf("Applied tx %s\n", tx.Hash())
			addHistory(l.state[tx.Sender], tx)
			if tx.Recipient != tx.Sender {
				addHistory(l.state[tx.Recipient], tx)
			}
		}
	}

	if err := l.appendWAL(b); err != nil {
		return err
	}
	if b.Slot%1000 == 0 {
		_ = l.SaveSnapshot("ledger/snapshot.gob")
	}
	fmt.Printf("[ledger] Block %d applied\n", b.Slot)
	return nil
}

func (l *Ledger) NewSession() *Session {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return &Session{
		ledger: l,
		view: &LedgerView{
			base:    l.state,
			overlay: make(map[string]*types.SnapshotAccount),
		},
	}
}

func (l *Ledger) GetAccount(addr string) *types.Account {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.state[addr]
}

// Apply transaction to ledger (after verifying signature)
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

func (l *Ledger) GetTxByHash(hash string) (*transaction.Transaction, error) {
	tx, err := l.txStore.GetByHash(hash)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

// TODO: need to optimize this by using BadgerDB
func (l *Ledger) GetTxs(addr string, limit uint32, offset uint32, filter uint32) (uint32, []*transaction.Transaction) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	txs := make([]*transaction.Transaction, 0)
	acc, ok := l.state[addr]
	if !ok {
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

func (l *Ledger) appendWAL(b *block.Block) error {
	path := "ledger/wal.log"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("write WAL fail: %v", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, entry := range b.Entries {
		txs, err := l.txStore.GetBatch(entry.TxHashes)
		if err != nil {
			return err
		}

		for _, tx := range txs {
			_ = enc.Encode(types.TxRecord{
				Slot:      b.Slot,
				Amount:    tx.Amount,
				Sender:    tx.Sender,
				Recipient: tx.Recipient,
				Timestamp: tx.Timestamp,
				TextData:  tx.TextData,
				Type:      tx.Type,
				Nonce:     tx.Nonce,
			})
		}
	}
	return nil
}

func (l *Ledger) SaveSnapshot(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(l.state); err != nil {
		return err
	}
	defer f.Close()
	return os.Rename(tmp, path)
}

func (l *Ledger) LoadLedger() error {
	// 1. snapshot
	if s, err := os.Open("ledger/snapshot.gob"); err == nil {
		_ = gob.NewDecoder(s).Decode(&l.state)
		s.Close()
	}

	// 2. replay WAL
	if w, err := os.Open("ledger/wal.log"); err == nil {
		dec := json.NewDecoder(w)
		var rec types.TxRecord
		for dec.Decode(&rec) == nil {
			_ = applyTx(l.state, &transaction.Transaction{
				Type:      rec.Type,
				Sender:    rec.Sender,
				Recipient: rec.Recipient,
				Amount:    rec.Amount,
				Timestamp: rec.Timestamp,
				TextData:  rec.TextData,
				Nonce:     rec.Nonce,
			})
		}
		w.Close()
	}
	return nil
}

// LedgerView is a view of the ledger to verify block before applying it to the ledger.
type LedgerView struct {
	base    map[string]*types.Account
	overlay map[string]*types.SnapshotAccount
	mu      sync.RWMutex // Add mutex for overlay map protection
}

func (lv *LedgerView) loadForRead(addr string) (*types.SnapshotAccount, bool) {
	lv.mu.RLock()
	if acc, ok := lv.overlay[addr]; ok {
		lv.mu.RUnlock()
		return acc, true
	}
	lv.mu.RUnlock()

	if base, ok := lv.base[addr]; ok {
		cp := types.SnapshotAccount{Balance: base.Balance, Nonce: base.Nonce}
		lv.mu.Lock()
		lv.overlay[addr] = &cp
		lv.mu.Unlock()
		return &cp, true
	}
	return nil, false
}

func (lv *LedgerView) loadOrCreate(addr string) *types.SnapshotAccount {
	if acc, ok := lv.loadForRead(addr); ok {
		return acc
	}
	cp := types.SnapshotAccount{Balance: 0, Nonce: 0}
	lv.mu.Lock()
	lv.overlay[addr] = &cp
	lv.mu.Unlock()
	return &cp
}

func (lv *LedgerView) ApplyTx(tx *transaction.Transaction) error {
	// Validate zero amount transfers
	if tx.Amount == 0 {
		return fmt.Errorf("zero amount transfers are not allowed")
	}

	// Validate sender account existence
	if _, exists := lv.loadForRead(tx.Sender); !exists {
		return fmt.Errorf("sender account does not exist: %s", tx.Sender)
	}

	sender := lv.loadOrCreate(tx.Sender)
	recipient := lv.loadOrCreate(tx.Recipient)

	if sender.Balance < tx.Amount {
		return fmt.Errorf("insufficient balance")
	}
	// Strict nonce validation to prevent duplicate transactions (Ethereum standard)
	if tx.Nonce != sender.Nonce+1 {
		return fmt.Errorf("invalid nonce: expected %d, got %d", sender.Nonce+1, tx.Nonce)
	}

	sender.Balance -= tx.Amount
	recipient.Balance += tx.Amount
	sender.Nonce = tx.Nonce
	return nil
}

type Session struct {
	ledger *Ledger
	view   *LedgerView
}

// Copy session with clone overlay
func (s *Session) CopyWithOverlayClone() *Session {
	s.view.mu.RLock()
	overlayCopy := make(map[string]*types.SnapshotAccount, len(s.view.overlay))
	for k, v := range s.view.overlay {
		accCopy := *v
		overlayCopy[k] = &accCopy
	}
	s.view.mu.RUnlock()

	return &Session{
		ledger: s.ledger,
		view: &LedgerView{
			base:    s.view.base,
			overlay: overlayCopy,
			mu:      sync.RWMutex{}, // Initialize mutex for new session
		},
	}
}

// Session API for filtering valid transactions
func (s *Session) FilterValid(raws [][]byte) ([]*transaction.Transaction, []error) {
	valid := make([]*transaction.Transaction, 0, len(raws))
	errs := make([]error, 0)
	for _, r := range raws {
		tx, err := utils.ParseTx(r)
		if err != nil || !tx.Verify() {
			fmt.Printf("Invalid tx: %v, %+v\n", err, tx)
			errs = append(errs, fmt.Errorf("sig/format: %w", err))
			continue
		}
		if err := s.view.ApplyTx(tx); err != nil {
			fmt.Printf("Invalid tx: %v, %+v\n", err, tx)
			errs = append(errs, err)
			continue
		}
		valid = append(valid, tx)
	}
	return valid, errs
}
