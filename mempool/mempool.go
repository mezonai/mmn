package mempool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/types"
)

type Mempool struct {
	mu          sync.RWMutex // Changed to RWMutex for better concurrency
	txsBuf      map[string][]byte
	txOrder     []string // Maintain FIFO order
	max         int
	broadcaster interfaces.Broadcaster
	ledger      interfaces.Ledger            // Add ledger for validation
	nonceTxMap  map[string]map[uint64]string // sender -> nonce -> txHash for duplicate nonce detection
}

func NewMempool(max int, broadcaster interfaces.Broadcaster, ledger interfaces.Ledger) *Mempool {
	return &Mempool{
		txsBuf:      make(map[string][]byte, max),
		txOrder:     make([]string, 0, max),
		max:         max,
		broadcaster: broadcaster,
		ledger:      ledger,
		nonceTxMap:  make(map[string]map[uint64]string),
	}
}

func (mp *Mempool) AddTx(tx *types.Transaction, broadcast bool) (string, error) {
	// Generate hash first (read-only operation)
	txBytes := tx.Bytes()
	txHash := tx.Hash()

	// Validate transaction before adding to mempool
	if err := mp.validateTransaction(tx); err != nil {
		fmt.Printf("Dropping invalid tx %s: %v\n", txHash, err)
		return "", err
	}

	// Quick check for duplicate using read lock
	mp.mu.RLock()
	if _, exists := mp.txsBuf[txHash]; exists {
		mp.mu.RUnlock()
		fmt.Println("Dropping duplicate tx", txHash)
		return "", fmt.Errorf("duplicate transaction")
	}

	// Check for duplicate nonce from same sender
	if senderNonces, exists := mp.nonceTxMap[tx.Sender]; exists {
		if existingTxHash, nonceExists := senderNonces[tx.Nonce]; nonceExists {
			mp.mu.RUnlock()
			fmt.Printf("Dropping duplicate nonce tx %s (sender: %s, nonce: %d, existing: %s)\n",
				txHash, tx.Sender[:8], tx.Nonce, existingTxHash)
			return "", fmt.Errorf("duplicate nonce %d for sender %s", tx.Nonce, tx.Sender[:8])
		}
	}

	// Check if mempool is full
	if len(mp.txsBuf) >= mp.max {
		mp.mu.RUnlock()
		fmt.Println("Dropping full mempool")
		return "", fmt.Errorf("mempool full")
	}
	mp.mu.RUnlock()

	// Now acquire write lock for actual insertion
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Double-check after acquiring write lock (for race conditions)
	if _, exists := mp.txsBuf[txHash]; exists {
		fmt.Println("Dropping duplicate tx (double-check)", txHash)
		return "", fmt.Errorf("duplicate transaction")
	}

	// Double-check nonce after acquiring write lock
	if senderNonces, exists := mp.nonceTxMap[tx.Sender]; exists {
		if existingTxHash, nonceExists := senderNonces[tx.Nonce]; nonceExists {
			fmt.Printf("Dropping duplicate nonce tx (double-check) %s (sender: %s, nonce: %d, existing: %s)\n",
				txHash, tx.Sender[:8], tx.Nonce, existingTxHash)
			return "", fmt.Errorf("duplicate nonce %d for sender %s", tx.Nonce, tx.Sender[:8])
		}
	}

	if len(mp.txsBuf) >= mp.max {
		fmt.Println("Dropping full mempool (double-check)")
		return "", fmt.Errorf("mempool full")
	}

	fmt.Println("Adding tx", tx)
	mp.txsBuf[txHash] = txBytes
	mp.txOrder = append(mp.txOrder, txHash) // Add to order queue

	// Track nonce for duplicate detection
	if mp.nonceTxMap[tx.Sender] == nil {
		mp.nonceTxMap[tx.Sender] = make(map[uint64]string)
	}
	mp.nonceTxMap[tx.Sender][tx.Nonce] = txHash

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
	return txHash, nil
}

// validateTransaction performs comprehensive transaction validation
func (mp *Mempool) validateTransaction(tx *types.Transaction) error {
	// 1. Verify signature (skip for testing if signature is "test_signature")
	if !tx.Verify() {
		return fmt.Errorf("invalid signature")
	}

	// 2. Check for zero amount
	if tx.Amount == 0 {
		return fmt.Errorf("zero amount not allowed")
	}

	// 3. Check sender account exists and get current state
	if mp.ledger == nil {
		return fmt.Errorf("ledger not available for validation")
	}

	senderAccount := mp.ledger.GetAccount(tx.Sender)
	if senderAccount == nil {
		return fmt.Errorf("sender account %s does not exist", tx.Sender)
	}

	// 4. Check nonce is exactly next expected value
	if tx.Nonce != senderAccount.Nonce+1 {
		return fmt.Errorf("invalid nonce: expected %d, got %d", senderAccount.Nonce+1, tx.Nonce)
	}

	// 5. Check sufficient balance
	if senderAccount.Balance < tx.Amount {
		return fmt.Errorf("insufficient balance: have %d, need %d", senderAccount.Balance, tx.Amount)
	}

	return nil
}

// PullBatch optimizes batch extraction with FIFO order
func (mp *Mempool) PullBatch(batchSize int) [][]byte {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	batch := make([][]byte, 0, batchSize)
	removedTxHashes := make([]string, 0, batchSize)

	// Process transactions in FIFO order
	for i, txHash := range mp.txOrder {
		if i >= batchSize {
			break
		}

		if txData, exists := mp.txsBuf[txHash]; exists {
			batch = append(batch, txData)
			removedTxHashes = append(removedTxHashes, txHash)
		}
	}

	// Remove processed transactions from both map and order slice
	if len(batch) > 0 {
		// Remove from map and clean up nonce tracking
		for i := 0; i < len(batch) && i < len(mp.txOrder); i++ {
			txHash := mp.txOrder[i]
			delete(mp.txsBuf, txHash)

			// Clean up nonce tracking
			mp.cleanupNonceTracking(txHash)
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

// cleanupNonceTracking removes nonce tracking for a specific transaction
func (mp *Mempool) cleanupNonceTracking(txHash string) {
	// Find and remove the transaction from nonce tracking
	for sender, nonces := range mp.nonceTxMap {
		for nonce, hash := range nonces {
			if hash == txHash {
				delete(nonces, nonce)
				// If no more nonces for this sender, remove the sender entry
				if len(nonces) == 0 {
					delete(mp.nonceTxMap, sender)
				}
				return
			}
		}
	}
}

// RemoveProcessedTxs - remove processed transactions from mempool
func (mp *Mempool) RemoveProcessedTxs(processedHashSet map[string]struct{}) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	newTxsOrder := make([]string, 0)
	for _, txHash := range mp.txOrder {
		if _, exists := processedHashSet[txHash]; exists {
			delete(mp.txsBuf, txHash)
			mp.cleanupNonceTracking(txHash)
			continue
		}
		newTxsOrder = append(newTxsOrder, txHash)
	}

	mp.txOrder = newTxsOrder
}
