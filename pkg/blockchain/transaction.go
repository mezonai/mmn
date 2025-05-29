package blockchain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// Transaction represents a simple transaction.
type Transaction struct {
	Sender       string                 `json:"sender"`
	Recipient    string                 `json:"recipient"`
	Amount       float64                `json:"amount"`
	Timestamp    int64                  `json:"timestamp"`
	ContractName string                 `json:"contract_name,omitempty"`
	Method       string                 `json:"method,omitempty"`
	Params       map[string]interface{} `json:"params,omitempty"`
	Signature    string                 `json:"signature,omitempty"` // Digital signature (hex-encoded).
	Nonce        int                    `json:"nonce,omitempty"`     // Optional nonce to prevent replay.
	// In a more complete system, you might include digital signatures.
}

// NewTransaction creates a new transaction and sets its timestamp.
func NewTransaction(sender, recipient string, amount float64, nonce int) *Transaction {
	return &Transaction{
		Sender:    sender,
		Recipient: recipient,
		Amount:    amount,
		Timestamp: time.Now().Unix(),
		Nonce:     nonce,
	}
}

// String returns a string representation for signing.
func (tx *Transaction) String() string {
	return fmt.Sprintf("%s:%s:%f:%d:%d", tx.Sender, tx.Recipient, tx.Amount, tx.Timestamp, tx.Nonce)
}

// CalculateHash returns the SHA‑256 hash of the transaction.
func (tx *Transaction) CalculateHash() string {
	record := fmt.Sprintf("%s%s%f%d", tx.Sender, tx.Recipient, tx.Amount, tx.Timestamp)
	h := sha256.Sum256([]byte(record))
	return hex.EncodeToString(h[:])
}

// TransactionPool holds pending transactions.
type TransactionPool struct {
	Transactions []*Transaction
}

// AddTransaction appends a new transaction to the pool.
func (tp *TransactionPool) AddTransaction(tx *Transaction) {
	tp.Transactions = append(tp.Transactions, tx)
}

// Clear empties the transaction pool.
func (tp *TransactionPool) Clear() {
	tp.Transactions = []*Transaction{}
}
