package ledger

import (
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mezonai/mmn/blockstore"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/types"
)

type Ledger struct {
	state       map[string]*types.Account // address (public key hex) → account
	mu          sync.RWMutex
	txStore     blockstore.TxStore
	txMetaStore blockstore.TxMetaStore
}

func NewLedger(txStore blockstore.TxStore, txMetaStore blockstore.TxMetaStore) *Ledger {
	return &Ledger{state: make(map[string]*types.Account), txStore: txStore, txMetaStore: txMetaStore}
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
			if err := applyTx(l.state, tx); err != nil {
				l.state[tx.Sender].Nonce++
				txMetas = append(txMetas, types.NewTxMeta(tx, b.Slot, hex.EncodeToString(b.Hash[:]), types.TxStatusFailed, err.Error()))
				fmt.Printf("apply fail: %v\n", err)
				continue
			}
			fmt.Printf("Applied tx %s\n", tx.Hash())
			txMetas = append(txMetas, types.NewTxMeta(tx, b.Slot, hex.EncodeToString(b.Hash[:]), types.TxStatusSuccess, ""))
			addHistory(l.state[tx.Sender], tx)
			if tx.Recipient != tx.Sender {
				addHistory(l.state[tx.Recipient], tx)
			}
		}

		if len(txMetas) > 0 {
			l.txMetaStore.StoreBatch(txMetas)
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

func (l *Ledger) GetAccount(addr string) *types.Account {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.state[addr]
}

// Apply transaction to ledger (after verifying signature)
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

// TODO: need to optimize this by using BadgerDB
func (l *Ledger) GetTxs(addr string, limit uint32, offset uint32, filter uint32) (uint32, []*types.Transaction) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	txs := make([]*types.Transaction, 0)
	acc, ok := l.state[addr]
	if !ok {
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
			_ = applyTx(l.state, &types.Transaction{
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
