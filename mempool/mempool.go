package mempool

import (
	"context"
	"fmt"
	"github.com/mezonai/mmn/logx"
	"sync"
	"time"

	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/transaction"
)

// Constants for zero-fee blockchain optimization
const (
	MaxPendingPerSender = 60               // Max future transactions per sender
	StaleTimeout        = 10 * time.Minute // Remove old pending transactions
	MaxFutureNonce      = 64               // Max nonce distance from current
)

// PendingTransaction wraps a transaction with timestamp for timeout management
type PendingTransaction struct {
	Tx        *transaction.Transaction
	Timestamp time.Time
}

type Mempool struct {
	mu          sync.RWMutex // Changed to RWMutex for better concurrency
	txsBuf      map[string][]byte
	txOrder     []string // Maintain FIFO order
	max         int
	broadcaster interfaces.Broadcaster
	ledger      interfaces.Ledger // Add ledger for validation

	pendingTxs    map[string]map[uint64]*PendingTransaction // sender -> nonce -> pending tx
	readyQueue    []*transaction.Transaction                // ready-to-process transactions
	accountNonces map[string]uint64                         // cached account nonces for efficiency
	eventRouter *events.EventRouter          // Event router for transaction status updates
}

func NewMempool(max int, broadcaster interfaces.Broadcaster, ledger interfaces.Ledger, eventRouter *events.EventRouter) *Mempool {
	return &Mempool{
		txsBuf:      make(map[string][]byte, max),
		txOrder:     make([]string, 0, max),
		max:         max,
		broadcaster: broadcaster,
		ledger:      ledger,

		// Initialize zero-fee optimization fields
		pendingTxs:    make(map[string]map[uint64]*PendingTransaction),
		readyQueue:    make([]*transaction.Transaction, 0),
		accountNonces: make(map[string]uint64),
		eventRouter: eventRouter,
	}
}

func (mp *Mempool) AddTx(tx *transaction.Transaction, broadcast bool) (string, error) {
	// Generate hash first (read-only operation)
	txHash := tx.Hash()

	// Initial basic validation (signature, format, etc.)
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

	if len(mp.txsBuf) >= mp.max {
		fmt.Println("Dropping full mempool (double-check)")
		return "", fmt.Errorf("mempool full")
	}

	fmt.Println("Adding tx", tx)

	// Determine if the transaction is ready or pending
	senderAccount, err := mp.ledger.GetAccount(tx.Sender)
	if err != nil || senderAccount == nil {
		return "", fmt.Errorf("failed to get sender account: %w", err)
	}
	currentNonce := mp.getCurrentNonce(tx.Sender, senderAccount.Nonce)
	isReady := tx.Nonce == currentNonce+1

	// Validate balance accounting for existing pending/ready transactions
	if err := mp.validateBalance(tx); err != nil {
		fmt.Printf("Dropping tx %s due to insufficient balance: %s\n", txHash, err.Error())
		return "", err
	}

	if isReady {
		// Add to ready queue for immediate processing
		mp.readyQueue = append(mp.readyQueue, tx)
		fmt.Printf("Added ready tx %s (sender: %s, nonce: %d)\n", txHash, tx.Sender[:8], tx.Nonce)
	} else {
		// Add to pending transactions
		if mp.pendingTxs[tx.Sender] == nil {
			mp.pendingTxs[tx.Sender] = make(map[uint64]*PendingTransaction)
		}
		mp.pendingTxs[tx.Sender][tx.Nonce] = &PendingTransaction{
			Tx:        tx,
			Timestamp: time.Now(),
		}
		fmt.Printf("Added pending tx %s (sender: %s, nonce: %d, expected: %d)\n",
			txHash, tx.Sender[:8], tx.Nonce, currentNonce+1)
	}

	// Always add to txsBuf and txOrder for compatibility
	mp.txsBuf[txHash] = tx.Bytes()
	mp.txOrder = append(mp.txOrder, txHash)

	// Publish event for transaction status tracking
	if mp.eventRouter != nil {
		event := events.NewTransactionAddedToMempool(txHash, tx)
		mp.eventRouter.PublishTransactionEvent(event)
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
	return txHash, nil
}

// validateTransaction performs comprehensive transaction validation
// getCurrentNonce returns the current nonce for a sender, using cached value if available
func (mp *Mempool) getCurrentNonce(sender string, ledgerNonce uint64) uint64 {
	if cachedNonce, exists := mp.accountNonces[sender]; exists {
		// Use the higher of cached or ledger nonce (in case ledger was updated externally)
		if cachedNonce > ledgerNonce {
			return cachedNonce
		}
	}
	return ledgerNonce
}

func (mp *Mempool) validateBalance(tx *transaction.Transaction) error {
	senderAccount, err := mp.ledger.GetAccount(tx.Sender)
	if err != nil || senderAccount == nil {
		return fmt.Errorf("could not get sender accont: %w", err)
	}
	availableBalance := senderAccount.Balance

	// Subtract amounts from pending transactions to get true available balance
	if pendingNonces, exists := mp.pendingTxs[tx.Sender]; exists {
		for _, pendingTx := range pendingNonces {
			availableBalance -= pendingTx.Tx.Amount
		}
	}

	// Also subtract amounts from ready queue transactions for this sender
	for _, readyTx := range mp.readyQueue {
		if readyTx.Sender == tx.Sender {
			availableBalance -= readyTx.Amount
		}
	}

	if availableBalance < tx.Amount {
		return fmt.Errorf("insufficient available balance: have %d (after pending: %d), need %d",
			senderAccount.Balance, availableBalance, tx.Amount)
	}

	return nil
}

func (mp *Mempool) validateTransaction(tx *transaction.Transaction) error {
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

	senderAccount, err := mp.ledger.GetAccount(tx.Sender)
	if err != nil {
		return fmt.Errorf("could not get sender account %s", tx.Sender)
	}
	if senderAccount == nil {
		return fmt.Errorf("sender account %s does not exist", tx.Sender)
	}

	// 4. Enhanced nonce validation for zero-fee blockchain
	// Get current nonce (use cached value if available, otherwise from ledger)
	currentNonce := mp.getCurrentNonce(tx.Sender, senderAccount.Nonce)

	// Reject old transactions (nonce too low)
	if tx.Nonce <= currentNonce {
		return fmt.Errorf("nonce too low: expected > %d, got %d", currentNonce, tx.Nonce)
	}

	// Prevent spam with reasonable future nonce limit
	if tx.Nonce > currentNonce+MaxFutureNonce {
		return fmt.Errorf("nonce too high: max allowed %d, got %d",
			currentNonce+MaxFutureNonce, tx.Nonce)
	}

	// 6. Check pending transaction limits per sender
	if pendingNonces, exists := mp.pendingTxs[tx.Sender]; exists {
		if len(pendingNonces) >= MaxPendingPerSender {
			return fmt.Errorf("too many pending transactions for sender %s: max %d",
				tx.Sender[:8], MaxPendingPerSender)
		}

		// 7. Check for duplicate nonce in pending transactions
		if _, nonceExists := pendingNonces[tx.Nonce]; nonceExists {
			return fmt.Errorf("duplicate nonce %d for sender %s in pending transactions",
				tx.Nonce, tx.Sender[:8])
		}
	}

	// 8. Check for duplicate nonce in ready queue
	for _, readyTx := range mp.readyQueue {
		if readyTx.Sender == tx.Sender && readyTx.Nonce == tx.Nonce {
			return fmt.Errorf("duplicate nonce %d for sender %s in ready queue",
				tx.Nonce, tx.Sender[:8])
		}
	}

	return nil
}

// PullBatch implements smart dependency resolution for zero-fee blockchain
func (mp *Mempool) PullBatch(batchSize int) [][]byte {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	batch := make([][]byte, 0, batchSize)
	processedCount := 0

	// Clean up stale transactions first
	mp.cleanupStaleTransactions()

	// Keep processing until no more ready transactions or batch is full
	for processedCount < batchSize {
		readyTxs := mp.findReadyTransactions(batchSize - processedCount)
		if len(readyTxs) == 0 {
			fmt.Printf("No more ready transactions found, processed %d\n", processedCount)
			break // No more ready transactions
		}

		fmt.Printf("Found %d ready transactions\n", len(readyTxs))
		for _, tx := range readyTxs {
			batch = append(batch, tx.Bytes())
			mp.removeTransaction(tx)
			mp.updateAccountNonce(tx.Sender, tx.Nonce)
			processedCount++

			fmt.Printf("Processed tx %s (sender: %s, nonce: %d)\n",
				tx.Hash(), tx.Sender[:8], tx.Nonce)
		}

		// Check if any pending transactions became ready after processing
		mp.promotePendingTransactions()
	}

	fmt.Printf("PullBatch returning %d transactions\n", len(batch))
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

// findReadyTransactions finds transactions that are ready to be processed
func (mp *Mempool) findReadyTransactions(maxCount int) []*transaction.Transaction {
	readyTxs := make([]*transaction.Transaction, 0, maxCount)

	// First, process from ready queue
	for len(mp.readyQueue) > 0 && len(readyTxs) < maxCount {
		tx := mp.readyQueue[0]
		mp.readyQueue = mp.readyQueue[1:]
		readyTxs = append(readyTxs, tx)
	}

	// Then check pending transactions for newly ready ones
	for sender, pendingMap := range mp.pendingTxs {
		if len(readyTxs) >= maxCount {
			break
		}

		senderAccount, err := mp.ledger.GetAccount(sender)
		if err != nil {
			logx.Error("MEMPOOL", "findReadyTransactions: failed to get account for sender %s", sender)
			continue
		}
		currentNonce := mp.getCurrentNonce(sender, senderAccount.Nonce)
		expectedNonce := currentNonce + 1

		if pendingTx, exists := pendingMap[expectedNonce]; exists {
			readyTxs = append(readyTxs, pendingTx.Tx)
			delete(pendingMap, expectedNonce)
			if len(pendingMap) == 0 {
				delete(mp.pendingTxs, sender)
			}
		}
	}

	return readyTxs
}

// removeTransaction removes a transaction from all tracking structures
func (mp *Mempool) removeTransaction(tx *transaction.Transaction) {
	txHash := tx.Hash()

	// Remove from txsBuf
	delete(mp.txsBuf, txHash)

	// Remove from txOrder
	for i, hash := range mp.txOrder {
		if hash == txHash {
			mp.txOrder = append(mp.txOrder[:i], mp.txOrder[i+1:]...)
			break
		}
	}
}

// updateAccountNonce updates the cached nonce for an account
func (mp *Mempool) updateAccountNonce(sender string, nonce uint64) {
	mp.accountNonces[sender] = nonce
	fmt.Printf("Updated cached nonce for %s to %d\n", sender[:8], nonce)
}

// promotePendingTransactions checks if any pending transactions became ready
func (mp *Mempool) promotePendingTransactions() {
	for sender, pendingMap := range mp.pendingTxs {
		senderAccount, err := mp.ledger.GetAccount(sender)
		if err != nil {
			logx.Error("MEMPOOL", "promotePendingTransactions: failed to get account for sender %s", sender)
			continue
		}
		currentNonce := mp.getCurrentNonce(sender, senderAccount.Nonce)
		expectedNonce := currentNonce + 1

		if pendingTx, exists := pendingMap[expectedNonce]; exists {
			mp.readyQueue = append(mp.readyQueue, pendingTx.Tx)
			delete(pendingMap, expectedNonce)
			if len(pendingMap) == 0 {
				delete(mp.pendingTxs, sender)
			}
			fmt.Printf("Promoted pending tx to ready (sender: %s, nonce: %d)\n",
				sender[:8], expectedNonce)
		}
	}
}

// cleanupStaleTransactions removes transactions that have been pending too long
func (mp *Mempool) cleanupStaleTransactions() {
	now := time.Now()
	staleThreshold := now.Add(-StaleTimeout)

	for sender, pendingMap := range mp.pendingTxs {
		for nonce, pendingTx := range pendingMap {
			if pendingTx.Timestamp.Before(staleThreshold) {
				// Remove stale transaction
				mp.removeTransaction(pendingTx.Tx)
				delete(pendingMap, nonce)
				fmt.Printf("Removed stale transaction (sender: %s, nonce: %d)\n",
					sender[:8], nonce)
			}
		}
		if len(pendingMap) == 0 {
			delete(mp.pendingTxs, sender)
		}
	}
}

// PeriodicCleanup performs comprehensive mempool maintenance
// This should be called periodically by the node to maintain mempool health
func (mp *Mempool) PeriodicCleanup() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	fmt.Println("Starting periodic mempool cleanup...")

	// Clean up stale transactions
	mp.cleanupStaleTransactions()

	// Promote any newly ready transactions
	mp.promotePendingTransactions()

	// Log current mempool state
	totalPending := 0
	for _, pendingMap := range mp.pendingTxs {
		totalPending += len(pendingMap)
	}

	fmt.Printf("Mempool cleanup complete - Ready: %d, Pending: %d, Total: %d\n",
		len(mp.readyQueue), totalPending, len(mp.txsBuf))
}

// GetLargestPendingNonce returns the largest nonce among pending transactions for a given sender
// Returns 0 if no pending transactions exist for the sender
func (mp *Mempool) GetLargestPendingNonce(sender string) uint64 {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	pendingMap, exists := mp.pendingTxs[sender]
	if !exists || len(pendingMap) == 0 {
		return 0
	}

	var largestNonce uint64 = 0
	for nonce := range pendingMap {
		if nonce > largestNonce {
			largestNonce = nonce
		}
	}

	return largestNonce
}

// GetMempoolStats returns current mempool statistics
func (mp *Mempool) GetMempoolStats() map[string]interface{} {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	totalPending := 0
	pendingBySender := make(map[string]int)

	for sender, pendingMap := range mp.pendingTxs {
		count := len(pendingMap)
		totalPending += count
		pendingBySender[sender[:8]] = count
	}

	return map[string]interface{}{
		"ready_transactions":   len(mp.readyQueue),
		"pending_transactions": totalPending,
		"total_transactions":   len(mp.txsBuf),
		"pending_by_sender":    pendingBySender,
		"unique_senders":       len(mp.pendingTxs),
	}
}
