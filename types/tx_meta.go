package types

const (
	TxStatusFailed  = 0
	TxStatusSuccess = 1
)

type TransactionMeta struct {
	TxHash string `json:"tx_hash"`
	Status int32  `json:"status"`
	Error  string `json:"error"`
}

func NewTxMeta(tx *Transaction, status int32, err string) *TransactionMeta {
	return &TransactionMeta{
		TxHash: tx.Hash(),
		Status: status,
		Error:  err,
	}
}
