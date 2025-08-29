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
	processingTxs map[string]*Transaction

	// senderTxs maps sender address to list of transaction hashes
	senderTxs map[string][]string

	mu sync.RWMutex
}

// NewTransactionTracker creates a new transaction tracker instance
func NewTransactionTracker() *TransactionTracker {
	return &TransactionTracker{
		processingTxs: make(map[string]*Transaction),
		senderTxs:     make(map[string][]string),
	}
}

// TrackProcessingTransaction starts tracking a transaction that was pulled from mempool
func (t *TransactionTracker) TrackProcessingTransaction(tx *Transaction) {
	t.mu.Lock()
	defer t.mu.Unlock()

	txHash := tx.Hash()
	t.processingTxs[txHash] = tx
	t.senderTxs[tx.Sender] = append(t.senderTxs[tx.Sender], txHash)

	logx.Info("TRACKER", fmt.Sprintf("Tracking processing transaction: %s (sender: %s, nonce: %d)",
		txHash, tx.Sender[:8], tx.Nonce))
}

// RemoveTransaction removes a transaction from tracking
func (t *TransactionTracker) RemoveTransaction(txHash string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	tx, exists := t.processingTxs[txHash]
	if !exists {
		return
	}

	delete(t.processingTxs, txHash)
	t.senderTxs[tx.Sender] = remove(t.senderTxs[tx.Sender], txHash)
}

// GetLargestProcessingNonce returns the largest nonce currently being processed for a sender
// This is used by getCurrentNonce to account for transactions in the pipeline
func (t *TransactionTracker) GetLargestProcessingNonce(sender string) uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	txHashes := t.senderTxs[sender]
	if len(txHashes) == 0 {
		return 0
	}

	largestNonce := uint64(0)
	for _, txHash := range txHashes {
		tx := t.processingTxs[txHash]
		if tx.Nonce > largestNonce {
			largestNonce = tx.Nonce
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
