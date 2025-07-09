package ledger

import (
	"fmt"
	"mmn/block"
)

type Account struct {
	Balance uint64
	Nonce   uint64
}

type Ledger struct {
	state map[string]*Account // address (public key hex) → account
}

func NewLedger() *Ledger {
	return &Ledger{state: make(map[string]*Account)}
}

// Initialize initial account
func (l *Ledger) CreateAccount(addr string, balance uint64) {
	l.state[addr] = &Account{Balance: balance, Nonce: 0}
}

// Apply transaction to ledger (after verifying signature)
func (l *Ledger) ApplyTx(tx *Transaction) error {
	from, ok := l.state[tx.From]
	if !ok {
		return fmt.Errorf("from account not found")
	}
	to, ok := l.state[tx.To]
	if !ok {
		l.state[tx.To] = &Account{}
		to = l.state[tx.To]
	}
	if from.Balance < tx.Amount {
		return fmt.Errorf("insufficient balance")
	}
	if tx.Nonce != from.Nonce+1 {
		return fmt.Errorf("invalid nonce: got %d want %d", tx.Nonce, from.Nonce+1)
	}
	// Apply transfer
	from.Balance -= tx.Amount
	to.Balance += tx.Amount
	from.Nonce++
	return nil
}

// Query balance
func (l *Ledger) Balance(addr string) uint64 {
	acc, ok := l.state[addr]
	if !ok {
		return 0
	}
	return acc.Balance
}

// Apply all transactions in block
func (l *Ledger) ApplyBlock(b *block.Block) error {
	for _, entry := range b.Entries {
		for _, rawTx := range entry.Transactions {
			tx, err := ParseTx(rawTx)
			if err != nil {
				return fmt.Errorf("cannot parse tx: %v", err)
			}
			if !tx.Verify() {
				return fmt.Errorf("tx signature invalid")
			}
			if err := l.ApplyTx(tx); err != nil {
				return fmt.Errorf("tx fail: %v", err)
			}
		}
	}
	return nil
}
