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
	TxExtraInfo() string
	Transaction() *transaction.Transaction
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

func (e *TransactionAddedToMempool) TxExtraInfo() string {
	return e.tx.ExtraInfo
}

// TransactionIncludedInBlock event when a transaction is included in a block
type TransactionIncludedInBlock struct {
	tx        *transaction.Transaction
	blockSlot uint64
	blockHash string
	timestamp time.Time
}

func NewTransactionIncludedInBlock(tx *transaction.Transaction, blockSlot uint64, blockHash string) *TransactionIncludedInBlock {
	return &TransactionIncludedInBlock{
		tx:        tx,
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
	return e.tx.Hash()
}

func (e *TransactionIncludedInBlock) BlockSlot() uint64 {
	return e.blockSlot
}

func (e *TransactionIncludedInBlock) BlockHash() string {
	return e.blockHash
}

func (e *TransactionIncludedInBlock) TxExtraInfo() string {
	return e.tx.ExtraInfo
}

func (e *TransactionIncludedInBlock) Transaction() *transaction.Transaction {
	return e.tx
}

// TransactionFinalized event when a transaction is finalized (block is finalized)
type TransactionFinalized struct {
	tx        *transaction.Transaction
	blockSlot uint64
	blockHash string
	timestamp time.Time
}

func NewTransactionFinalized(tx *transaction.Transaction, blockSlot uint64, blockHash string) *TransactionFinalized {
	return &TransactionFinalized{
		tx:        tx,
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
	return e.tx.Hash()
}

func (e *TransactionFinalized) BlockSlot() uint64 {
	return e.blockSlot
}

func (e *TransactionFinalized) BlockHash() string {
	return e.blockHash
}

func (e *TransactionFinalized) TxExtraInfo() string {
	return e.tx.ExtraInfo
}

func (e *TransactionFinalized) Transaction() *transaction.Transaction {
	return e.tx
}

// TransactionFailed event when a transaction fails validation
type TransactionFailed struct {
	tx           *transaction.Transaction
	errorMessage string
	timestamp    time.Time
}

func NewTransactionFailed(tx *transaction.Transaction, errorMessage string) *TransactionFailed {
	return &TransactionFailed{
		tx:           tx,
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
	return e.tx.Hash()
}

func (e *TransactionFailed) ErrorMessage() string {
	return e.errorMessage
}

func (e *TransactionFailed) TxExtraInfo() string {
	return e.tx.ExtraInfo
}

func (e *TransactionFailed) Transaction() *transaction.Transaction {
	return e.tx
}
