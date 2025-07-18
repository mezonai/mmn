package ledger

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"mmn/block"
	"os"
	"path/filepath"
	"sync"
)

type TxRecord struct {
	Slot      uint64
	Amount    uint64
	Sender    string
	Recipient string
	Timestamp int64
	TextData  string
	Type      int
	Nonce     uint64
}

type Account struct {
	Address string
	Balance uint64
	Nonce   uint64
	History []TxRecord
}

type Ledger struct {
	state      map[string]*Account // address (public key hex) → account
	faucetAddr string
	mu         sync.RWMutex
}

func NewLedger(faucetAddr string) *Ledger {
	return &Ledger{state: make(map[string]*Account), faucetAddr: faucetAddr}
}

// Initialize initial account
func (l *Ledger) CreateAccount(addr string, balance uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.state[addr] = &Account{Balance: balance, Nonce: 0}
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

func (l *Ledger) VerifyBlock(b *block.Block) error {
	l.mu.RLock()
	base := l.state
	l.mu.RUnlock()

	view := &LedgerView{
		base:    base,
		overlay: make(map[string]*SnapshotAccount),
	}
	for _, entry := range b.Entries {
		for _, raw := range entry.Transactions {
			tx, err := ParseTx(raw)
			if err != nil || !tx.Verify() {
				return fmt.Errorf("tx parse/sig fail: %v", err)
			}
			if err := view.ApplyTx(tx, l.faucetAddr); err != nil {
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
		for _, raw := range entry.Transactions {
			tx, err := ParseTx(raw)
			if err != nil || !tx.Verify() {
				return fmt.Errorf("tx parse/sig fail: %v", err)
			}
			if err := applyTx(l.state, tx, l.faucetAddr); err != nil {
				return fmt.Errorf("apply fail: %v", err)
			}
			rec := TxRecord{
				Slot:      b.Slot,
				Amount:    tx.Amount,
				Sender:    tx.Sender,
				Recipient: tx.Recipient,
				Timestamp: tx.Timestamp,
				TextData:  tx.TextData,
				Type:      tx.Type,
				Nonce:     tx.Nonce,
			}
			addHistory(l.state[tx.Sender], rec)
			if tx.Recipient != tx.Sender {
				addHistory(l.state[tx.Recipient], rec)
			}
		}
	}

	if err := l.appendWAL(b); err != nil {
		return err
	}
	if b.Slot%1000 == 0 {
		_ = l.saveSnapshot("ledger/snapshot.gob")
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
			overlay: make(map[string]*SnapshotAccount),
		},
	}
}

func (l *Ledger) GetAccount(addr string) *Account {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.state[addr]
}

// Apply transaction to ledger (after verifying signature)
func applyTx(state map[string]*Account, tx *Transaction, faucetAddr string) error {
	sender, ok := state[tx.Sender]
	if !ok {
		state[tx.Sender] = &Account{Address: tx.Sender, Balance: 0, Nonce: 0, History: make([]TxRecord, 0)}
		sender = state[tx.Sender]
	}
	recipient, ok := state[tx.Recipient]
	if !ok {
		state[tx.Recipient] = &Account{Address: tx.Recipient, Balance: 0, Nonce: 0, History: make([]TxRecord, 0)}
		recipient = state[tx.Recipient]
	}

	if tx.Type == TxTypeFaucet {
		if tx.Sender != faucetAddr {
			return fmt.Errorf("faucet tx from non-faucet address")
		}
		fmt.Printf("[applyTx] Faucet tx: %+v\n", tx)
		sender.Balance -= tx.Amount
		recipient.Balance += tx.Amount
		return nil
	}

	if sender.Balance < tx.Amount {
		return fmt.Errorf("insufficient balance")
	}
	if tx.Nonce != sender.Nonce+1 {
		return fmt.Errorf("bad nonce: got %d want %d", tx.Nonce, sender.Nonce+1)
	}
	sender.Balance -= tx.Amount
	recipient.Balance += tx.Amount
	sender.Nonce++
	return nil
}

func addHistory(acc *Account, rec TxRecord) {
	acc.History = append(acc.History, rec)
}

func (l *Ledger) GetTxs(addr string) []TxRecord {
	l.mu.RLock()
	defer l.mu.RUnlock()

	txs := make([]TxRecord, 0)
	acc, ok := l.state[addr]
	if !ok {
		return txs
	}
	for i := len(acc.History) - 1; i >= 0; i-- {
		txs = append(txs, acc.History[i])
	}
	return txs
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
		for _, raw := range entry.Transactions {
			tx, _ := ParseTx(raw)
			_ = enc.Encode(TxRecord{
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

func (l *Ledger) saveSnapshot(path string) error {
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
		var rec TxRecord
		for dec.Decode(&rec) == nil {
			_ = applyTx(l.state, &Transaction{
				Type:      rec.Type,
				Sender:    rec.Sender,
				Recipient: rec.Recipient,
				Amount:    rec.Amount,
				Timestamp: rec.Timestamp,
				TextData:  rec.TextData,
				Nonce:     rec.Nonce,
			}, l.faucetAddr)
		}
		w.Close()
	}
	return nil
}

// LedgerView is a view of the ledger to verify block before applying it to the ledger.
type LedgerView struct {
	base    map[string]*Account
	overlay map[string]*SnapshotAccount
}

type SnapshotAccount struct {
	Balance uint64
	Nonce   uint64
}

func (lv *LedgerView) loadForRead(addr string) (*SnapshotAccount, bool) {
	if acc, ok := lv.overlay[addr]; ok {
		return acc, true
	}
	if base, ok := lv.base[addr]; ok {
		cp := SnapshotAccount{Balance: base.Balance, Nonce: base.Nonce}
		lv.overlay[addr] = &cp
		return &cp, true
	}
	return nil, false
}

func (lv *LedgerView) loadOrCreate(addr string) *SnapshotAccount {
	if acc, ok := lv.loadForRead(addr); ok {
		return acc
	}
	cp := SnapshotAccount{Balance: 0, Nonce: 0}
	lv.overlay[addr] = &cp
	return &cp
}

func (lv *LedgerView) ApplyTx(tx *Transaction, faucetAddr string) error {
	sender := lv.loadOrCreate(tx.Sender)
	recipient := lv.loadOrCreate(tx.Recipient)

	if tx.Type == TxTypeFaucet {
		if tx.Sender != faucetAddr {
			return fmt.Errorf("faucet tx from non-faucet address")
		}
		sender.Balance -= tx.Amount
		recipient.Balance += tx.Amount
		return nil
	}

	if sender.Balance < tx.Amount {
		return fmt.Errorf("insufficient balance")
	}
	if tx.Nonce != sender.Nonce+1 {
		return fmt.Errorf("bad nonce: got %d want %d", tx.Nonce, sender.Nonce+1)
	}

	sender.Balance -= tx.Amount
	recipient.Balance += tx.Amount
	sender.Nonce++
	return nil
}

type Session struct {
	ledger *Ledger
	view   *LedgerView
}

// Session API for filtering valid transactions
func (s *Session) FilterValid(raws [][]byte) ([][]byte, []error) {
	valid := make([][]byte, 0, len(raws))
	errs := make([]error, 0)
	for _, r := range raws {
		tx, err := ParseTx(r)
		if err != nil || !tx.Verify() {
			fmt.Printf("Invalid tx: %v, %+v\n", err, tx)
			errs = append(errs, fmt.Errorf("sig/format: %w", err))
			continue
		}
		if err := s.view.ApplyTx(tx, s.ledger.faucetAddr); err != nil {
			fmt.Printf("Invalid tx: %v, %+v\n", err, tx)
			errs = append(errs, err)
			continue
		}
		valid = append(valid, r)
	}
	return valid, errs
}
