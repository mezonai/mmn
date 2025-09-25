package types

import "github.com/mezonai/mmn/transaction"

const (
	TxStatusFailed    = 0
	TxStatusSuccess   = 1
	TxStatusProcessed = 2
)

type TransactionMeta struct {
	TxHash    string `json:"tx_hash"`
	Slot      uint64 `json:"slot"`
	BlockHash string `json:"block_hash"`
	Status    int32  `json:"status"`
	Error     string `json:"error"`
}

func NewTxMeta(tx *transaction.Transaction, slot uint64, blockHash string, status int32, err string) *TransactionMeta {
	return &TransactionMeta{
		TxHash:    tx.Hash(),
		Slot:      slot,
		BlockHash: blockHash,
		Status:    status,
		Error:     err,
	}
}
