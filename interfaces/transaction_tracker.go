package interfaces

import "github.com/mezonai/mmn/transaction"

// Tracking transactions between mempool and ledger
type TransactionTrackerInterface interface {
	TrackProcessingTransaction(tx *transaction.Transaction)
	RemoveTransaction(txHash string)
	GetLargestProcessingNonce(sender string) uint64
}
