package transaction

import (
	"fmt"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/monitoring"
)

const defaultRemovalThreshold = 10 * time.Minute

// TransactionTracker tracks transactions that are "floating" between mempool and ledger
// Only tracks transactions after they are pulled from mempool until they are applied to ledger
type TransactionTracker struct {
	// processingCache keeps transactions being processed with TTL to avoid leaks
	processingCache *ttlcache.Cache[string, *Transaction]

	// senderTxs maps sender address to list of transaction hashes
	senderTxs sync.Map

	// historyList is a blacklist to prevent re-tracking recently removed txs
	historyList sync.Map

	stopCh chan struct{}
}

// NewTransactionTracker creates a new transaction tracker instance
func NewTransactionTracker() *TransactionTracker {
	tt := &TransactionTracker{}
	tt.stopCh = make(chan struct{})

	// Initialize TTL cache for processing transactions
	// Default TTL to auto-expire forgotten items; can be tuned if needed
	processingTTL := 10 * time.Minute
	tt.processingCache = ttlcache.New(
		ttlcache.WithTTL[string, *Transaction](processingTTL),
	)

	exception.SafeGo("Start Processing Cache", func() {
		tt.processingCache.Start()
	})

	exception.SafeGo("StartAppliedCleanup", func() {
		tt.StartAppliedCleanup(10 * time.Minute)
	})
	return tt
}

// TrackProcessingTransaction starts tracking a transaction that was pulled from mempool
func (t *TransactionTracker) TrackProcessingTransaction(tx *Transaction) {
	txHash := tx.Hash()
	if t.IsRemoved(txHash) {
		t.historyList.Delete(txHash)
		return
	}

	t.processingCache.Set(txHash, tx, ttlcache.DefaultTTL)
	// Update sender transaction list
	var txHashes []string
	if existing, ok := t.senderTxs.Load(tx.Sender); ok {
		txHashes = append(existing.([]string), txHash)
		t.senderTxs.Store(tx.Sender, txHashes)
	} else {
		txHashes = []string{txHash}
		t.senderTxs.Store(tx.Sender, txHashes)
	}
	t.updateTrackerMetrics()
	logx.Info("TRACKER", fmt.Sprintf("Tracking processing transaction: %s (sender: %s, nonce: %d)",
		txHash, tx.Sender[:8], tx.Nonce))
}

// RemoveTransaction removes a transaction from tracking
func (t *TransactionTracker) RemoveTransaction(txHash string) {
	txItem := t.processingCache.Get(txHash)
	if txItem == nil {
		t.MarkRemoved(txHash)
		logx.Warn("TRACKER", fmt.Sprintf("Transaction %s does not exist in processingTxs", txHash))
		return
	}
	tx := txItem.Value()
	t.processingCache.Delete(txHash)

	// Update sender transaction list
	if existing, ok := t.senderTxs.Load(tx.Sender); ok {
		txHashes := existing.([]string)
		updatedHashes, isRemoved := remove(txHashes, txHash)
		if isRemoved {
			if len(updatedHashes) == 0 {
				t.senderTxs.Delete(tx.Sender)
			} else {
				t.senderTxs.Store(tx.Sender, updatedHashes)
			}
		}
	}
	t.updateTrackerMetrics()
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
		if item := t.processingCache.Get(txHash); item != nil {
			tx := item.Value()
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

func (t *TransactionTracker) updateTrackerMetrics() {
	monitoring.SetTrackerProcessingTx(int64(t.processingCache.Len()), "processing")

	uniqueSenders := make(map[string]struct{})
	t.processingCache.Range(func(itm *ttlcache.Item[string, *Transaction]) bool {
		tx := itm.Value()
		uniqueSenders[tx.Sender] = struct{}{}
		return true
	})
	monitoring.SetTrackerProcessingTx(int64(len(uniqueSenders)), "senders")
}
