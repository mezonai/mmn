// File: pkg/blockchain/ledger.go
package blockchain

import "errors"

// Ledger represents an account-based ledger.
type Ledger map[string]float64

// NewLedger creates a new ledger.
func NewLedger() Ledger {
	return make(Ledger)
}

// ProcessTransaction updates the ledger if the transaction is valid.
func (l Ledger) ProcessTransaction(tx *Transaction) error {
	// Check that the sender has enough balance.
	senderBalance := l[tx.Sender]
	if senderBalance < tx.Amount {
		return errors.New("insufficient funds")
	}
	l[tx.Sender] -= tx.Amount
	l[tx.Recipient] += tx.Amount
	return nil
}

// ProcessCoinbaseTransaction awards tokens to a miner.
func (l Ledger) ProcessCoinbaseTransaction(recipient string, reward float64) {
	l[recipient] += reward
}
