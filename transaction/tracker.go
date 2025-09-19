package transaction

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/monitoring"
)

const defaultRemovalThreshold = 10 * time.Minute

// TransactionTracker tracks transactions that are "floating" between mempool and ledger
// Only tracks transactions after they are pulled from mempool until they are applied to ledger
type TransactionTracker struct {
	// processingTxs maps transaction hash to transaction
	processingTxs sync.Map

	// senderTxs maps sender address to list of transaction hashes
	senderTxs sync.Map

	historyList sync.Map

	processingCount int64
	senderCount     int64
}

// NewTransactionTracker creates a new transaction tracker instance
func NewTransactionTracker() *TransactionTracker {
	tt := &TransactionTracker{}
	tt.StartAppliedCleanup(10 * time.Minute)
	return tt
}

// TrackProcessingTransaction starts tracking a transaction that was pulled from mempool
func (t *TransactionTracker) TrackProcessingTransaction(tx *Transaction) {
	txHash := tx.Hash()
	if t.IsRemoved(txHash) {
		t.historyList.Delete(txHash)
		return
	}
	_, loadedProcessing := t.processingTxs.LoadOrStore(txHash, tx)
	if !loadedProcessing {
		atomic.AddInt64(&t.processingCount, 1)
	}
	monitoring.SetTrackerProcessingTx(atomic.LoadInt64(&t.processingCount), "processing")
	// Update sender transaction list
	var txHashes []string
	if existing, ok := t.senderTxs.Load(tx.Sender); ok {
		txHashes = append(existing.([]string), txHash)
		t.senderTxs.Store(tx.Sender, txHashes)
	} else {
		txHashes = []string{txHash}
		t.senderTxs.Store(tx.Sender, txHashes)
		atomic.AddInt64(&t.senderCount, 1)
	}
	monitoring.SetTrackerProcessingTx(atomic.LoadInt64(&t.senderCount), "senders")
	logx.Info("TRACKER", fmt.Sprintf("Tracking processing transaction: %s (sender: %s, nonce: %d)",
		txHash, tx.Sender[:8], tx.Nonce))
}

// RemoveTransaction removes a transaction from tracking
func (t *TransactionTracker) RemoveTransaction(txHash string) {
	txInterface, exists := t.processingTxs.LoadAndDelete(txHash)
	if !exists {
		t.MarkRemoved(txHash)
		logx.Warn("TRACKER", fmt.Sprintf("Transaction %s does not exist in processingTxs", txHash))
		return
	}
	atomic.AddInt64(&t.processingCount, -1)
	tx := txInterface.(*Transaction)

	// Update sender transaction list
	if existing, ok := t.senderTxs.Load(tx.Sender); ok {
		txHashes := existing.([]string)
		updatedHashes, isRemoved := remove(txHashes, txHash)
		if isRemoved {
			if len(updatedHashes) == 0 {
				t.senderTxs.Delete(tx.Sender)
				atomic.AddInt64(&t.senderCount, -1)
			} else {
				t.senderTxs.Store(tx.Sender, updatedHashes)
			}
		}
	}
	monitoring.SetTrackerProcessingTx(atomic.LoadInt64(&t.processingCount), "processing")
	monitoring.SetTrackerProcessingTx(atomic.LoadInt64(&t.senderCount), "senders")
	logx.Info("TRACKER", fmt.Sprintf("Remove transaction: %s (sender: %s, nonce: %d)",
		txHash, tx.Sender[:8], tx.Nonce))
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

func remove(slice []string, item string) ([]string, bool) {
	for i, v := range slice {
		if v == item {
			return append(slice[:i], slice[i+1:]...), true
		}
	}
	return slice, false
}

// IsApplied checks if txHash was marked applied
func (t *TransactionTracker) IsRemoved(txHash string) bool {
	_, ok := t.historyList.Load(txHash)
	return ok
}

func (t *TransactionTracker) MarkRemoved(txHash string) {
	t.historyList.Store(txHash, time.Now().UnixMilli())
}

func (t *TransactionTracker) StartAppliedCleanup(interval time.Duration) {
	func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			func() {
				defer func() {
					if r := recover(); r != nil {
						logx.Warn("TRACKER", fmt.Sprintf("Cleanup iteration panicked:", r))
					}
				}()
				nowMs := time.Now().UnixMilli()
				thresholdMs := defaultRemovalThreshold.Milliseconds()
				t.historyList.Range(func(key, val any) bool {
					if _, ok := val.(bool); ok {
						t.historyList.Delete(key)
						return true
					}
					if ts, ok := val.(int64); ok && nowMs-ts >= thresholdMs {
						t.historyList.Delete(key)
					}
					return true
				})
			}()
		}
	}()
}
