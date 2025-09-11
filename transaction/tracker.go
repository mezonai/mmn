package transaction

import (
	"fmt"
	"sync"

	"github.com/mezonai/mmn/logx"
)

// TransactionTracker tracks transactions that are "floating" between mempool and ledger
// Only tracks transactions after they are pulled from mempool until they are applied to ledger
type TransactionTracker struct {
	// processingTxs maps transaction hash to transaction
	processingTxs sync.Map

	// senderTxs maps sender address to list of transaction hashes
	senderTxs sync.Map
}

// NewTransactionTracker creates a new transaction tracker instance
func NewTransactionTracker() *TransactionTracker {
	return &TransactionTracker{}
}

// TrackProcessingTransaction starts tracking a transaction that was pulled from mempool
func (t *TransactionTracker) TrackProcessingTransaction(tx *Transaction) {
	txHash := tx.Hash()
	t.processingTxs.Store(txHash, tx)

	// Update sender transaction list
	var txHashes []string
	if existing, ok := t.senderTxs.Load(tx.Sender); ok {
		txHashes = existing.([]string)
	}
	txHashes = append(txHashes, txHash)
	t.senderTxs.Store(tx.Sender, txHashes)

	logx.Info("TRACKER", fmt.Sprintf("Tracking processing transaction: %s (sender: %s, nonce: %d)",
		txHash, tx.Sender[:8], tx.Nonce))
}

// RemoveTransaction removes a transaction from tracking
func (t *TransactionTracker) RemoveTransaction(txHash string) {
	txInterface, exists := t.processingTxs.LoadAndDelete(txHash)
	if !exists {
		return
	}

	tx := txInterface.(*Transaction)

	// Update sender transaction list
	if existing, ok := t.senderTxs.Load(tx.Sender); ok {
		txHashes := existing.([]string)
		updatedHashes := remove(txHashes, txHash)
		if len(updatedHashes) == 0 {
			t.senderTxs.Delete(tx.Sender)
		} else {
			t.senderTxs.Store(tx.Sender, updatedHashes)
		}
	}
}

// GetLargestProcessingNonce returns the largest nonce currently being processed for a sender
// This is used by getCurrentNonce to account for transactions in the pipeline
func (t *TransactionTracker) GetLargestProcessingNonce(sender string) uint64 {
	txHashesInterface, ok := t.senderTxs.Load(sender)
	if !ok {
		return 0
	}

	txHashes := txHashesInterface.([]string)
	if len(txHashes) == 0 {
		return 0
	}

	largestNonce := uint64(0)
	for _, txHash := range txHashes {
		if txInterface, exists := t.processingTxs.Load(txHash); exists {
			tx := txInterface.(*Transaction)
			if tx.Nonce > largestNonce {
				largestNonce = tx.Nonce
			}
		}
	}

	return largestNonce
}

func remove(slice []string, item string) []string {
	for i, v := range slice {
		if v == item {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}
