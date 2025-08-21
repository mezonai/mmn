package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mezonai/mmn/blockstore"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/types"
	"github.com/mezonai/mmn/utils"
)

var (
	ErrAccountExisted = errors.New("account existed")
)

type Ledger struct {
	mu           sync.RWMutex
	txStore      blockstore.TxStore
	accountStore blockstore.AccountStore
}

func NewLedger(txStore blockstore.TxStore) *Ledger {
	return &Ledger{txStore: txStore}
}

// CreateAccount creates and stores a new account into db, return error if an account with the same addr existed
func (l *Ledger) CreateAccount(addr string, balance uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	existed, err := l.accountStore.ExistsByAddr(addr)
	if err != nil {
		return fmt.Errorf("could not check existence of account: %w", err)
	}
	if existed {
		return ErrAccountExisted
	}

	return l.accountStore.Store(&types.Account{
		Address: addr,
		Balance: balance,
		Nonce:   0,
	})
}

// CreateAccountsFromGenesis creates an account from genesis block (implements LedgerInterface)
func (l *Ledger) CreateAccountsFromGenesis(addrs []config.Address) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, addr := range addrs {
		err := l.CreateAccount(addr.Address, addr.Amount)
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

func (l *Ledger) VerifyBlock(b *block.BroadcastedBlock) error {
	l.mu.RLock()
	base := l.accountStore
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
			// load account state
			sender, err := l.accountStore.GetByAddr(tx.Sender)
			if err != nil {
				return err
			}
			recipient, err := l.accountStore.GetByAddr(tx.Recipient)
			if err != nil {
				return err
			}
			state := map[string]*types.Account{
				sender.Address:    sender,
				recipient.Address: recipient,
			}

			// try to apply tx
			if err := applyTx(state, tx); err != nil {
				return fmt.Errorf("apply fail: %v", err)
			}
			fmt.Printf("Applied tx %s\n", tx.Hash())
			addHistory(state[tx.Sender], tx)
			if tx.Recipient != tx.Sender {
				addHistory(state[tx.Recipient], tx)
			}

			// commit the update
			// TODO: not atomic operation, does db support transaction?
			if err := l.accountStore.Store(sender); err != nil {
				return err
			}
			if err := l.accountStore.Store(recipient); err != nil {
				return err
			}
		}
	}

	//if err := l.appendWAL(b); err != nil {
	//	return err
	//}
	//if b.Slot%1000 == 0 {
	//	_ = l.SaveSnapshot("ledger/snapshot.gob")
	//}
	fmt.Printf("[ledger] Block %d applied\n", b.Slot)
	return nil
}

func (l *Ledger) NewSession() *Session {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return &Session{
		ledger: l,
		view: &LedgerView{
			base:    l.accountStore,
			overlay: make(map[string]*types.SnapshotAccount),
		},
	}
}

// GetAccount returns account with addr (nil if not exist)
func (l *Ledger) GetAccount(addr string) (*types.Account, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.accountStore.GetByAddr(addr)
}

// Apply transaction to ledger (after verifying signature). NOTE: this does not perform persisting operation into db
func applyTx(state map[string]*types.Account, tx *types.Transaction) error {
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

func addHistory(acc *types.Account, tx *types.Transaction) {
	acc.History = append(acc.History, tx.Hash())
}

func (l *Ledger) GetTxByHash(hash string) (*types.Transaction, error) {
	tx, err := l.txStore.GetByHash(hash)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

func (l *Ledger) GetTxs(addr string, limit uint32, offset uint32, filter uint32) (uint32, []*types.Transaction) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	txs := make([]*types.Transaction, 0)
	acc, err := l.accountStore.GetByAddr(addr)
	if err != nil {
		return 0, txs
	}

	// filter type: 0: all, 1: sender, 2: recipient
	filteredHistory := make([]*types.Transaction, 0)
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

//func (l *Ledger) SaveSnapshot(path string) error {
//	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
//		return err
//	}
//
//	tmp := path + ".tmp"
//	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
//	if err != nil {
//		return err
//	}
//	if err := gob.NewEncoder(f).Encode(l.state); err != nil {
//		return err
//	}
//	defer f.Close()
//	return os.Rename(tmp, path)
//}
//
//func (l *Ledger) LoadLedger() error {
//	// 1. snapshot
//	if s, err := os.Open("ledger/snapshot.gob"); err == nil {
//		_ = gob.NewDecoder(s).Decode(&l.state)
//		s.Close()
//	}
//
//	// 2. replay WAL
//	if w, err := os.Open("ledger/wal.log"); err == nil {
//		dec := json.NewDecoder(w)
//		var rec types.TxRecord
//		for dec.Decode(&rec) == nil {
//			_ = applyTx(l.state, &types.Transaction{
//				Type:      rec.Type,
//				Sender:    rec.Sender,
//				Recipient: rec.Recipient,
//				Amount:    rec.Amount,
//				Timestamp: rec.Timestamp,
//				TextData:  rec.TextData,
//				Nonce:     rec.Nonce,
//			})
//		}
//		w.Close()
//	}
//	return nil
//}

// LedgerView is a view of the ledger to verify block before applying it to the ledger.
type LedgerView struct {
	base    blockstore.AccountStore
	overlay map[string]*types.SnapshotAccount
}

func (lv *LedgerView) loadForRead(addr string) (*types.SnapshotAccount, bool) {
	if acc, ok := lv.overlay[addr]; ok {
		return acc, true
	}
	base, err := lv.base.GetByAddr(addr)
	// TODO: re-verify this, will returning nil for error case be ok?
	if err != nil || base == nil {
		return nil, false
	}

	cp := types.SnapshotAccount{Balance: base.Balance, Nonce: base.Nonce}
	lv.overlay[addr] = &cp
	return &cp, true
}

func (lv *LedgerView) loadOrCreate(addr string) *types.SnapshotAccount {
	if acc, ok := lv.loadForRead(addr); ok {
		return acc
	}
	cp := types.SnapshotAccount{Balance: 0, Nonce: 0}
	lv.overlay[addr] = &cp
	return &cp
}

func (lv *LedgerView) ApplyTx(tx *types.Transaction) error {
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
	overlayCopy := make(map[string]*types.SnapshotAccount, len(s.view.overlay))
	for k, v := range s.view.overlay {
		accCopy := *v
		overlayCopy[k] = &accCopy
	}

	return &Session{
		ledger: s.ledger,
		view: &LedgerView{
			base:    s.view.base,
			overlay: overlayCopy,
		},
	}
}

// Session API for filtering valid transactions
func (s *Session) FilterValid(raws [][]byte) ([]*types.Transaction, []error) {
	valid := make([]*types.Transaction, 0, len(raws))
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
