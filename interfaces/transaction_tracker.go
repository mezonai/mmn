package interfaces

import "github.com/mezonai/mmn/transaction"

// TransactionTrackerInterface tracks transactions between the mempool and the ledger.
type TransactionTrackerInterface interface {
	TrackProcessingTransaction(tx *transaction.Transaction)
	RemoveTransaction(txHash string)
	GetTransaction(txHash string) (*transaction.Transaction, error)
}
