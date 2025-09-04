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
	txHash      string
	blockSlot   uint64
	blockHash   string
	timestamp   time.Time
	txExtraInfo string
}

func NewTransactionIncludedInBlock(txHash string, blockSlot uint64, blockHash string, txExtraInfo string) *TransactionIncludedInBlock {
	return &TransactionIncludedInBlock{
		txHash:      txHash,
		blockSlot:   blockSlot,
		blockHash:   blockHash,
		timestamp:   time.Now(),
		txExtraInfo: txExtraInfo,
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

func (e *TransactionIncludedInBlock) TxExtraInfo() string {
	return e.txExtraInfo
}

// TransactionFinalized event when a transaction is finalized (block is finalized)
type TransactionFinalized struct {
	txHash      string
	blockSlot   uint64
	blockHash   string
	timestamp   time.Time
	txExtraInfo string
}

func NewTransactionFinalized(txHash string, blockSlot uint64, blockHash string, txExtraInfo string) *TransactionFinalized {
	return &TransactionFinalized{
		txHash:      txHash,
		blockSlot:   blockSlot,
		blockHash:   blockHash,
		timestamp:   time.Now(),
		txExtraInfo: txExtraInfo,
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

func (e *TransactionFinalized) TxExtraInfo() string {
	return e.txExtraInfo
}

// TransactionFailed event when a transaction fails validation
type TransactionFailed struct {
	txHash       string
	errorMessage string
	timestamp    time.Time
	txExtraInfo  string
}

func NewTransactionFailed(txHash string, errorMessage string, txExtraInfo string) *TransactionFailed {
	return &TransactionFailed{
		txHash:       txHash,
		errorMessage: errorMessage,
		timestamp:    time.Now(),
		txExtraInfo:  txExtraInfo,
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

func (e *TransactionFailed) TxExtraInfo() string {
	return e.txExtraInfo
}
