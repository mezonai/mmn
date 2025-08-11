package mempool

import (
	"context"
	"fmt"
	"mmn/interfaces"
	"mmn/types"
	"sync"
	"time"
)

type Mempool struct {
	mu          sync.RWMutex // Changed to RWMutex for better concurrency
	txsBuf      map[string][]byte
	txOrder     []string // Maintain FIFO order
	max         int
	broadcaster interfaces.Broadcaster
	eventBus    *types.EventBus // Event bus for transaction status updates
}

func NewMempool(max int, broadcaster interfaces.Broadcaster, eventBus *types.EventBus) *Mempool {
	return &Mempool{
		txsBuf:      make(map[string][]byte, max),
		txOrder:     make([]string, 0, max),
		max:         max,
		broadcaster: broadcaster,
		eventBus:    eventBus,
	}
}

func (mp *Mempool) AddTx(tx *types.Transaction, broadcast bool) (string, bool) {
	// Generate hash first (read-only operation)
	txHash := tx.Hash()

	// Quick check for duplicate using read lock
	mp.mu.RLock()
	if _, exists := mp.txsBuf[txHash]; exists {
		mp.mu.RUnlock()
		fmt.Println("Dropping duplicate tx", txHash)
		return "", false // drop if duplicate
	}

	// Check if mempool is full
	if len(mp.txsBuf) >= mp.max {
		mp.mu.RUnlock()
		fmt.Println("Dropping full mempool")
		return "", false // drop if full
	}
	mp.mu.RUnlock()

	// Now acquire write lock for actual insertion
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Double-check after acquiring write lock (for race conditions)
	if _, exists := mp.txsBuf[txHash]; exists {
		fmt.Println("Dropping duplicate tx (double-check)", txHash)
		return "", false
	}

	if len(mp.txsBuf) >= mp.max {
		fmt.Println("Dropping full mempool (double-check)")
		return "", false
	}

	fmt.Println("Adding tx", tx)
	mp.txsBuf[txHash] = tx.Bytes()
	mp.txOrder = append(mp.txOrder, txHash) // Add to order queue

	// Publish event for transaction status tracking
	if mp.eventBus != nil {
		event := types.NewTransactionAddedToMempool(txHash, tx)
		mp.eventBus.Publish(event)
	}

	// Handle broadcast safely
	if broadcast && mp.broadcaster != nil {
		// Use goroutine to avoid blocking the critical path
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := mp.broadcaster.TxBroadcast(ctx, tx); err != nil {
				fmt.Printf("Broadcast error: %v\n", err)
			}
		}()
	}

	fmt.Println("Added tx", txHash)
	return txHash, true
}

// PullBatch optimizes batch extraction with FIFO order
func (mp *Mempool) PullBatch(batchSize int) [][]byte {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	batch := make([][]byte, 0, batchSize)

	// Process transactions in FIFO order
	for i, txHash := range mp.txOrder {
		if i >= batchSize {
			break
		}

		if txData, exists := mp.txsBuf[txHash]; exists {
			batch = append(batch, txData)
		}
	}

	// Remove processed transactions from both map and order slice
	if len(batch) > 0 {
		// Remove from map
		for i := 0; i < len(batch) && i < len(mp.txOrder); i++ {
			delete(mp.txsBuf, mp.txOrder[i])
		}

		// Remove from order slice
		mp.txOrder = mp.txOrder[len(batch):]
	}

	return batch
}

// Size uses read lock for better concurrency
func (mp *Mempool) Size() int {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return len(mp.txsBuf)
}

// New method: GetTransactionCount - read-only operation
func (mp *Mempool) GetTransactionCount() int {
	return mp.Size()
}

// New method: HasTransaction - check if transaction exists (read-only)
func (mp *Mempool) HasTransaction(txHash string) bool {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	_, exists := mp.txsBuf[txHash]
	return exists
}

// New method: GetTransaction - retrieve transaction data (read-only)
func (mp *Mempool) GetTransaction(txHash string) ([]byte, bool) {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	data, exists := mp.txsBuf[txHash]
	return data, exists
}

// New method: GetOrderedTransactions - get transactions in FIFO order (read-only)
func (mp *Mempool) GetOrderedTransactions() []string {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	// Return a copy to avoid external modification
	result := make([]string, len(mp.txOrder))
	copy(result, mp.txOrder)
	return result
}
