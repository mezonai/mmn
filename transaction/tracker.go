package transaction

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/monitoring"
)

const defaultRemovalThreshold = 10 * time.Minute

// TransactionTracker tracks transactions that are "floating" between mempool and ledger
// Only tracks transactions after they are pulled from mempool until they are applied to ledger
type TransactionTracker struct {
	processingCount int64

	// processingTxs maps transaction hash to transaction
	processingTxs sync.Map

	historyList sync.Map

	stopCh chan struct{}
}

// NewTransactionTracker creates a new transaction tracker instance
func NewTransactionTracker() *TransactionTracker {
	tt := &TransactionTracker{}
	tt.stopCh = make(chan struct{})
	exception.SafeGo("StartAppliedCleanup", func() {
		tt.StartAppliedCleanup(10 * time.Minute)
	})
	return tt
}

func (t *TransactionTracker) GetTransaction(txHash string) (*Transaction, error) {
	txInterface, exists := t.processingTxs.Load(txHash)
	if !exists {
		return nil, errors.New("transaction not found")
	}
	return txInterface.(*Transaction), nil
}

// TrackProcessingTransaction starts tracking a transaction that was pulled from mempool
func (t *TransactionTracker) TrackProcessingTransaction(tx *Transaction) {
	txHash := tx.Hash()
	if t.isRemoved(txHash) {
		t.historyList.Delete(txHash)
		return
	}
	_, loadedProcessing := t.processingTxs.LoadOrStore(txHash, tx)
	if !loadedProcessing {
		atomic.AddInt64(&t.processingCount, 1)
	}
	monitoring.SetTrackerProcessingTx(atomic.LoadInt64(&t.processingCount), "processing")

	logx.Debug("TRACKER", fmt.Sprintf("Tracking processing transaction: %s (sender: %s, nonce: %d)",
		txHash, tx.Sender[:8], tx.Nonce))
}

// RemoveTransaction removes a transaction from tracking
func (t *TransactionTracker) RemoveTransaction(txHash string) {
	txInterface, exists := t.processingTxs.LoadAndDelete(txHash)
	if !exists {
		t.markRemoved(txHash)
		logx.Warn("TRACKER", fmt.Sprintf("Transaction %s does not exist in processingTxs", txHash))
		return
	}
	atomic.AddInt64(&t.processingCount, -1)
	tx := txInterface.(*Transaction)

	monitoring.SetTrackerProcessingTx(atomic.LoadInt64(&t.processingCount), "processing")
	logx.Debug("TRACKER", fmt.Sprintf("Remove transaction: %s (sender: %s, nonce: %d)",
		txHash, tx.Sender[:8], tx.Nonce))
}

// IsApplied checks if txHash was marked applied
func (t *TransactionTracker) isRemoved(txHash string) bool {
	_, ok := t.historyList.Load(txHash)
	return ok
}

func (t *TransactionTracker) markRemoved(txHash string) {
	t.historyList.Store(txHash, time.Now().UnixMilli())
}

func (t *TransactionTracker) StartAppliedCleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			exception.SafeGo("cleanOldItemIsBlackList", func() {
				t.cleanOldItemIsBlackList()
			})
		}
	}
}

func (t *TransactionTracker) cleanOldItemIsBlackList() {
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
}
