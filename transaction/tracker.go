package transaction

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/allegro/bigcache"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/monitoring"
)

// TransactionTracker tracks transactions that are "floating" between mempool and ledger
// Only tracks transactions after they are pulled from mempool until they are applied to ledger
type TransactionTracker struct {
	processingCount int64

	// processingTxs maps transaction hash to transaction
	processingTxs sync.Map

	removedTxCache *bigcache.BigCache
}

// NewTransactionTracker creates a new transaction tracker instance
func NewTransactionTracker() (*TransactionTracker, error) {
	cacheConfig := bigcache.Config{
		Shards:           128,
		LifeWindow:       10 * time.Minute,
		CleanWindow:      15 * time.Minute,
		MaxEntrySize:     10,
		Verbose:          false,
		HardMaxCacheSize: 0,
	}

	removedTxCache, err := bigcache.NewBigCache(cacheConfig)
	if err != nil {
		return nil, err
	}

	tt := &TransactionTracker{
		removedTxCache: removedTxCache,
	}

	return tt, nil
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
	if _, err := t.removedTxCache.Get(txHash); err == nil {
		t.removedTxCache.Delete(txHash)
		return
	} else if err != bigcache.ErrEntryNotFound {
		logx.Error("TRACKER", fmt.Sprintf("Failed to get removedTxCache for transaction %s: %v", txHash, err))
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
		err := t.removedTxCache.Set(txHash, []byte{1})
		if err != nil {
			logx.Error("TRACKER", fmt.Sprintf("Failed to set removedTxCache for transaction %s: %v", txHash, err))
		}
		logx.Warn("TRACKER", fmt.Sprintf("Transaction %s does not exist in processingTxs", txHash))
		return
	}
	atomic.AddInt64(&t.processingCount, -1)
	tx := txInterface.(*Transaction)

	monitoring.SetTrackerProcessingTx(atomic.LoadInt64(&t.processingCount), "processing")
	logx.Debug("TRACKER", fmt.Sprintf("Remove transaction: %s (sender: %s, nonce: %d)",
		txHash, tx.Sender[:8], tx.Nonce))
}
