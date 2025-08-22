package events

import (
	"time"

	"github.com/mezonai/mmn/transaction"
)

// EventType is an enum-like string type for blockchain events
type EventType string

const (
	EventTransactionAddedToMempool  EventType = "TransactionAddedToMempool"
	EventTransactionIncludedInBlock EventType = "TransactionIncludedInBlock"
	EventTransactionFinalized       EventType = "TransactionFinalized"
	EventTransactionFailed          EventType = "TransactionFailed"
)

// BlockchainEvent represents any event that occurs in the blockchain
type BlockchainEvent interface {
	Type() EventType
	Timestamp() time.Time
	TxHash() string
}

// TransactionAddedToMempool event when a transaction is added to mempool
type TransactionAddedToMempool struct {
	txHash    string
	tx        *transaction.Transaction
	timestamp time.Time
}

func NewTransactionAddedToMempool(txHash string, tx *transaction.Transaction) *TransactionAddedToMempool {
	return &TransactionAddedToMempool{
		txHash:    txHash,
		tx:        tx,
		timestamp: time.Now(),
	}
}

func (e *TransactionAddedToMempool) Type() EventType {
	return EventTransactionAddedToMempool
}

func (e *TransactionAddedToMempool) Timestamp() time.Time {
	return e.timestamp
}

func (e *TransactionAddedToMempool) TxHash() string {
	return e.txHash
}

func (e *TransactionAddedToMempool) Transaction() *transaction.Transaction {
	return e.tx
}

// TransactionIncludedInBlock event when a transaction is included in a block
type TransactionIncludedInBlock struct {
	txHash    string
	blockSlot uint64
	blockHash string
	timestamp time.Time
}

func NewTransactionIncludedInBlock(txHash string, blockSlot uint64, blockHash string) *TransactionIncludedInBlock {
	return &TransactionIncludedInBlock{
		txHash:    txHash,
		blockSlot: blockSlot,
		blockHash: blockHash,
		timestamp: time.Now(),
	}
}

func (e *TransactionIncludedInBlock) Type() EventType {
	return EventTransactionIncludedInBlock
}

func (e *TransactionIncludedInBlock) Timestamp() time.Time {
	return e.timestamp
}

func (e *TransactionIncludedInBlock) TxHash() string {
	return e.txHash
}

func (e *TransactionIncludedInBlock) BlockSlot() uint64 {
	return e.blockSlot
}

func (e *TransactionIncludedInBlock) BlockHash() string {
	return e.blockHash
}

// TransactionFinalized event when a transaction is finalized (block is finalized)
type TransactionFinalized struct {
	txHash    string
	blockSlot uint64
	blockHash string
	timestamp time.Time
}

func NewTransactionFinalized(txHash string, blockSlot uint64, blockHash string) *TransactionFinalized {
	return &TransactionFinalized{
		txHash:    txHash,
		blockSlot: blockSlot,
		blockHash: blockHash,
		timestamp: time.Now(),
	}
}

func (e *TransactionFinalized) Type() EventType {
	return EventTransactionFinalized
}

func (e *TransactionFinalized) Timestamp() time.Time {
	return e.timestamp
}

func (e *TransactionFinalized) TxHash() string {
	return e.txHash
}

func (e *TransactionFinalized) BlockSlot() uint64 {
	return e.blockSlot
}

func (e *TransactionFinalized) BlockHash() string {
	return e.blockHash
}

// TransactionFailed event when a transaction fails validation
type TransactionFailed struct {
	txHash       string
	errorMessage string
	timestamp    time.Time
}

func NewTransactionFailed(txHash string, errorMessage string) *TransactionFailed {
	return &TransactionFailed{
		txHash:       txHash,
		errorMessage: errorMessage,
		timestamp:    time.Now(),
	}
}

func (e *TransactionFailed) Type() EventType {
	return EventTransactionFailed
}

func (e *TransactionFailed) Timestamp() time.Time {
	return e.timestamp
}

func (e *TransactionFailed) TxHash() string {
	return e.txHash
}

func (e *TransactionFailed) ErrorMessage() string {
	return e.errorMessage
}
