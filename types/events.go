package types

import (
	"time"
)

// BlockchainEvent represents any event that occurs in the blockchain
type BlockchainEvent interface {
	Type() string
	Timestamp() time.Time
	TxHash() string
}

// TransactionAddedToMempool event when a transaction is added to mempool
type TransactionAddedToMempool struct {
	txHash    string
	tx        *Transaction
	timestamp time.Time
}

func NewTransactionAddedToMempool(txHash string, tx *Transaction) *TransactionAddedToMempool {
	return &TransactionAddedToMempool{
		txHash:    txHash,
		tx:        tx,
		timestamp: time.Now(),
	}
}

func (e *TransactionAddedToMempool) Type() string {
	return "TransactionAddedToMempool"
}

func (e *TransactionAddedToMempool) Timestamp() time.Time {
	return e.timestamp
}

func (e *TransactionAddedToMempool) TxHash() string {
	return e.txHash
}

func (e *TransactionAddedToMempool) Transaction() *Transaction {
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

func (e *TransactionIncludedInBlock) Type() string {
	return "TransactionIncludedInBlock"
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

func (e *TransactionFailed) Type() string {
	return "TransactionFailed"
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

// BlockFinalized event when a block is finalized
type BlockFinalized struct {
	blockSlot uint64
	blockHash string
	timestamp time.Time
}

func NewBlockFinalized(blockSlot uint64, blockHash string) *BlockFinalized {
	return &BlockFinalized{
		blockSlot: blockSlot,
		blockHash: blockHash,
		timestamp: time.Now(),
	}
}

func (e *BlockFinalized) Type() string {
	return "BlockFinalized"
}

func (e *BlockFinalized) Timestamp() time.Time {
	return e.timestamp
}

func (e *BlockFinalized) TxHash() string {
	return "" // Block events don't have a specific tx hash
}

func (e *BlockFinalized) BlockSlot() uint64 {
	return e.blockSlot
}

func (e *BlockFinalized) BlockHash() string {
	return e.blockHash
}
