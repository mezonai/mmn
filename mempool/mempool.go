package mempool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/monitoring"
	"github.com/mezonai/mmn/zkverify"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/logx"

	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/transaction"
)

// Constants for zero-fee blockchain optimization
const (
	MaxPendingPerSender = 60              // Max future transactions per sender
	StaleTimeout        = 5 * time.Minute // Remove old pending transactions
	MaxFutureNonce      = 64              // Max nonce distance from current
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

	pendingTxs  map[string]map[uint64]*PendingTransaction // sender -> nonce -> pending tx
	readyQueue  []*transaction.Transaction                // ready-to-process transactions
	eventRouter *events.EventRouter                       // Event router for transaction status updates
	txTracker   interfaces.TransactionTrackerInterface    // Transaction state tracker
	zkVerify    *zkverify.ZkVerify                        // Zk verify for zk transactions
}

func NewMempool(max int, broadcaster interfaces.Broadcaster, ledger interfaces.Ledger, eventRouter *events.EventRouter,
	txTracker interfaces.TransactionTrackerInterface, zkVerify *zkverify.ZkVerify) *Mempool {
	return &Mempool{
		txsBuf:      make(map[string][]byte, max),
		txOrder:     make([]string, 0, max),
		max:         max,
		broadcaster: broadcaster,
		ledger:      ledger,

		// Initialize zero-fee optimization fields
		pendingTxs:  make(map[string]map[uint64]*PendingTransaction),
		readyQueue:  make([]*transaction.Transaction, 0),
		eventRouter: eventRouter,
		txTracker:   txTracker,
		zkVerify:    zkVerify,
	}
}

func (mp *Mempool) GetZkVerify() *zkverify.ZkVerify {
	return mp.zkVerify
}

func (mp *Mempool) AddTx(tx *transaction.Transaction, broadcast bool) (string, error) {
	// Generate hash first (read-only operation)
	txHash := tx.Hash()
	monitoring.IncreaseReceivedTxCount()

	// Quick check for duplicate using read lock
	mp.mu.RLock()
	if _, exists := mp.txsBuf[txHash]; exists {
		mp.mu.RUnlock()
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping duplicate tx %s", txHash))
		monitoring.RecordRejectedTx(monitoring.TxDuplicated)
		return "", fmt.Errorf("duplicate transaction")
	}

	// Check if mempool is full
	if len(mp.txsBuf) >= mp.max {
		mp.mu.RUnlock()
		logx.Error("MEMPOOL", "Dropping full mempool")
		monitoring.RecordRejectedTx(monitoring.TxMempoolFull)
		return "", fmt.Errorf("mempool full")
	}
	mp.mu.RUnlock()

	// Now acquire write lock for validation and insertion
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Double-check after acquiring write lock (for race conditions)
	if _, exists := mp.txsBuf[txHash]; exists {
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping duplicate tx (double-check) %s", txHash))
		monitoring.RecordRejectedTx(monitoring.TxDuplicated)
		return "", fmt.Errorf("duplicate transaction")
	}

	if len(mp.txsBuf) >= mp.max {
		logx.Error("MEMPOOL", "Dropping full mempool (double-check)")
		monitoring.RecordRejectedTx(monitoring.TxMempoolFull)
		return "", fmt.Errorf("mempool full")
	}

	// Validate transaction INSIDE the write lock
	if err := mp.validateTransaction(tx); err != nil {
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping invalid tx %s: %v", txHash, err))
		return "", err
	}

	logx.Info("MEMPOOL", fmt.Sprintf("Adding tx %+v", tx))

	// Determine if transaction is ready or pending
	mp.processTransactionToQueue(tx)

	// Always add to txsBuf and txOrder for compatibility
	mp.txsBuf[txHash] = tx.Bytes()
	monitoring.SetMempoolSize(mp.Size())
	mp.txOrder = append(mp.txOrder, txHash)

	// Publish event for transaction status tracking
	if mp.eventRouter != nil {
		event := events.NewTransactionAddedToMempool(txHash, tx)
		mp.eventRouter.PublishTransactionEvent(event)
		monitoring.IncreaseIngressTpsCount()
	}

	// Handle broadcast safely
	if broadcast && mp.broadcaster != nil {
		// Use goroutine to avoid blocking the critical path
		exception.SafeGo("TxBroadcast", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := mp.broadcaster.TxBroadcast(ctx, tx); err != nil {
				logx.Error("MEMPOOL", fmt.Sprintf("Broadcast error: %v", err))
			}
		})
	}

	logx.Info("MEMPOOL", fmt.Sprintf("Added tx %s", txHash))

	return txHash, nil
}

func (mp *Mempool) processTransactionToQueue(tx *transaction.Transaction) {
	txHash := tx.Hash()
	account, err := mp.ledger.GetAccount(tx.Sender)
	if err != nil {
		logx.Error("MEMPOOL", fmt.Sprintf("could not get account: %v", err))
		return // Handle error appropriately based on context
	}
	currentNonce := account.Nonce
	isReady := tx.Nonce == currentNonce+1

	if isReady {
		// Add to ready queue for immediate processing
		mp.readyQueue = append(mp.readyQueue, tx)
		logx.Info("MEMPOOL", fmt.Sprintf("Added ready tx %s (sender: %s, nonce: %d, expected: %d)",
			txHash, tx.Sender[:8], tx.Nonce, currentNonce+1))
	} else {
		// Add to pending transactions
		if mp.pendingTxs[tx.Sender] == nil {
			mp.pendingTxs[tx.Sender] = make(map[uint64]*PendingTransaction)
		}
		mp.pendingTxs[tx.Sender][tx.Nonce] = &PendingTransaction{
			Tx:        tx,
			Timestamp: time.Now(),
		}
		logx.Info("MEMPOOL", fmt.Sprintf("Added pending tx %s (sender: %s, nonce: %d, expected: %d)",
			txHash, tx.Sender[:8], tx.Nonce, currentNonce+1))
	}
}

func (mp *Mempool) validateBalance(tx *transaction.Transaction) error {
	if tx == nil {
		return fmt.Errorf("transaction cannot be nil")
	}
	if tx.Amount == nil {
		return fmt.Errorf("transaction amount cannot be nil")
	}

	senderAccount, err := mp.ledger.GetAccount(tx.Sender)
	if err != nil || senderAccount == nil {
		return fmt.Errorf("could not get sender accont: %w", err)
	}

	// Add nil check for balance
	if senderAccount.Balance == nil {
		logx.Warn("MEMPOOL", fmt.Sprintf("Sender account %s has nil balance, treating as zero", tx.Sender[:8]))
		return fmt.Errorf("sender account has invalid balance")
	}

	availableBalance := new(uint256.Int).Set(senderAccount.Balance)

	// Subtract amounts from pending transactions to get true available balance
	if pendingNonces, exists := mp.pendingTxs[tx.Sender]; exists {
		for _, pendingTx := range pendingNonces {
			if pendingTx != nil && pendingTx.Tx != nil && pendingTx.Tx.Amount != nil {
				availableBalance.Sub(availableBalance, pendingTx.Tx.Amount)
			}
		}
	}

	// Also subtract amounts from ready queue transactions for this sender
	for _, readyTx := range mp.readyQueue {
		if readyTx != nil && readyTx.Sender == tx.Sender && readyTx.Amount != nil {
			availableBalance.Sub(availableBalance, readyTx.Amount)
		}
	}

	if availableBalance.Cmp(tx.Amount) < 0 {
		return fmt.Errorf("insufficient available balance: have %s (after pending: %s), need %s",
			senderAccount.Balance.String(), availableBalance.String(), tx.Amount.String())
	}

	return nil
}

// Stateless validation, simple for tx
func (mp *Mempool) validateTransaction(tx *transaction.Transaction) error {
	// 1. Verify signature (skip for testing if signature is "test_signature")
	if !tx.Verify(mp.zkVerify) {
		monitoring.RecordRejectedTx(monitoring.TxInvalidSignature)
		return fmt.Errorf("invalid signature or zk proof")
	}

	// 2. Check for zero amount
	if tx.Amount == nil || tx.Amount.IsZero() {
		monitoring.RecordRejectedTx(monitoring.TxRejectedUnknown)
		return fmt.Errorf("zero amount not allowed")
	}

	// 3. Check sender account exists and get current state
	if mp.ledger == nil {
		monitoring.RecordRejectedTx(monitoring.TxRejectedUnknown)
		return fmt.Errorf("ledger not available for validation")
	}

	senderAccount, err := mp.ledger.GetAccount(tx.Sender)
	if err != nil {
		monitoring.RecordRejectedTx(monitoring.TxRejectedUnknown)
		return fmt.Errorf("could not get sender account %s", tx.Sender)
	}
	if senderAccount == nil {
		monitoring.RecordRejectedTx(monitoring.TxSenderNotExist)
		return fmt.Errorf("sender account %s does not exist", tx.Sender)
	}

	// 4. Enhanced nonce validation for zero-fee blockchain
	// Get current nonce (use cached value if available, otherwise from ledger)
	currentNonce := senderAccount.Nonce

	// Reject old transactions (nonce too low)
	if tx.Nonce <= currentNonce {
		monitoring.RecordRejectedTx(monitoring.TxInvalidNonce)
		return fmt.Errorf("nonce too low: expected > %d, got %d", currentNonce, tx.Nonce)
	}

	// Prevent spam with reasonable future nonce limit
	if tx.Nonce > currentNonce+MaxFutureNonce {
		monitoring.RecordRejectedTx(monitoring.TxInvalidNonce)
		return fmt.Errorf("nonce too high: max allowed %d, got %d",
			currentNonce+MaxFutureNonce, tx.Nonce)
	}

	// 6. Check pending transaction limits per sender
	if pendingNonces, exists := mp.pendingTxs[tx.Sender]; exists {
		if len(pendingNonces) >= MaxPendingPerSender {
			monitoring.RecordRejectedTx(monitoring.TxTooManyPending)
			return fmt.Errorf("too many pending transactions for sender %s: max %d",
				tx.Sender[:8], MaxPendingPerSender)
		}

		// 7. Check for duplicate nonce in pending transactions
		if _, nonceExists := pendingNonces[tx.Nonce]; nonceExists {
			monitoring.RecordRejectedTx(monitoring.TxInvalidNonce)
			return fmt.Errorf("duplicate nonce %d for sender %s in pending transactions",
				tx.Nonce, tx.Sender[:8])
		}
	}

	// 8. Check for duplicate nonce in ready queue
	for _, readyTx := range mp.readyQueue {
		if readyTx.Sender == tx.Sender && readyTx.Nonce == tx.Nonce {
			monitoring.RecordRejectedTx(monitoring.TxInvalidNonce)
			return fmt.Errorf("duplicate nonce %d for sender %s in ready queue",
				tx.Nonce, tx.Sender[:8])
		}
	}

	// 9. Validate balance accounting for existing pending/ready transactions
	if err := mp.validateBalance(tx); err != nil {
		monitoring.RecordRejectedTx(monitoring.TxInsufficientBalance)
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping tx %s due to insufficient balance: %s", tx.Hash(), err.Error()))
		return err
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
	// mp.cleanupStaleTransactions()

	// Keep processing until no more ready transactions or batch is full
	for processedCount < batchSize {
		readyTxs := mp.findReadyTransactions(batchSize - processedCount)
		if len(readyTxs) == 0 {
			logx.Info("MEMPOOL", fmt.Sprintf("No more ready transactions found, processed %d", processedCount))
			break // No more ready transactions
		}

		logx.Info("MEMPOOL", fmt.Sprintf("Found %d ready transactions", len(readyTxs)))
		for _, tx := range readyTxs {
			batch = append(batch, tx.Bytes())
			txHash := tx.Hash()

			// Track transaction as processing when pulled from mempool
			if mp.txTracker != nil {
				mp.txTracker.TrackProcessingTransaction(tx)
			}

			mp.removeTransaction(tx)
			processedCount++

			logx.Info("MEMPOOL", fmt.Sprintf("Processed tx %s (sender: %s, nonce: %d)",
				txHash, tx.Sender[:8], tx.Nonce))
		}
		// Check if any pending transactions became ready after processing
		mp.promotePendingTransactions(readyTxs)
	}

	logx.Info("MEMPOOL", fmt.Sprintf("PullBatch returning %d transactions", len(batch)))
	return batch
}

func (mp *Mempool) promotePendingTransactions(readyTxs []*transaction.Transaction) {
	for _, tx := range readyTxs {
		account, err := mp.ledger.GetAccount(tx.Sender)
		if err != nil {
			logx.Error("MEMPOOL", fmt.Sprintf("Error getting account for sender %s: %v", tx.Sender, err))
			return
		}
		currentNonce := account.Nonce
		expectedNonce := currentNonce + 1

		if pendingMap, exists := mp.pendingTxs[tx.Sender]; exists {
			if pendingTx, hasNonce := pendingMap[expectedNonce]; hasNonce {
				// Move transaction from pending to ready queue
				mp.readyQueue = append(mp.readyQueue, pendingTx.Tx)
				delete(pendingMap, expectedNonce)

				// Cleanup empty pending maps
				if len(pendingMap) == 0 {
					delete(mp.pendingTxs, tx.Sender)
				}

				logx.Info("MEMPOOL", fmt.Sprintf("Promoted pending tx for sender %s with nonce %d",
					tx.Sender[:8], expectedNonce))
			}
		}
	}
}

// Size returns current size of mempool without locking
func (mp *Mempool) Size() int {
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

		account, err := mp.ledger.GetAccount(sender)
		if err != nil {
			logx.Error("MEMPOOL", fmt.Sprintf("Error getting account for sender %s: %v", sender, err))
			continue
		}
		currentNonce := account.Nonce
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
	monitoring.SetMempoolSize(mp.Size())

	// Remove from txOrder
	for i, hash := range mp.txOrder {
		if hash == txHash {
			mp.txOrder = append(mp.txOrder[:i], mp.txOrder[i+1:]...)
			break
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
				logx.Info("MEMPOOL", fmt.Sprintf("Removed stale transaction (sender: %s, nonce: %d)",
					sender[:8], nonce))
			}
		}
		if len(pendingMap) == 0 {
			delete(mp.pendingTxs, sender)
		}
	}
}

func (mp *Mempool) BlockCleanup(block *block.BroadcastedBlock) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Track removed transactions for logging
	removedCount := 0

	// Iterate through all entries in the block and clean up all transaction references
	for _, entry := range block.Entries {
		for _, tx := range entry.Transactions {
			// Track transaction as processing when remove from mempool
			if mp.txTracker != nil {
				mp.txTracker.TrackProcessingTransaction(tx)
			}

			txHash := tx.Hash()

			// Remove from main transaction buffer
			if _, exists := mp.txsBuf[txHash]; exists {
				delete(mp.txsBuf, txHash)
				monitoring.SetMempoolSize(mp.Size())

				// Remove from txOrder
				for i, hash := range mp.txOrder {
					if hash == txHash {
						mp.txOrder = append(mp.txOrder[:i], mp.txOrder[i+1:]...)
						break
					}
				}

				removedCount++
			}

			// Remove from ready queue
			for i := len(mp.readyQueue) - 1; i >= 0; i-- {
				if mp.readyQueue[i].Hash() == txHash {
					mp.readyQueue = append(mp.readyQueue[:i], mp.readyQueue[i+1:]...)
				}
			}

			// Remove from pending transactions
			for sender, nonceTxs := range mp.pendingTxs {
				for nonce, pendingTx := range nonceTxs {
					if pendingTx.Tx.Hash() == txHash {
						delete(nonceTxs, nonce)
						// Clean up empty sender map
						if len(nonceTxs) == 0 {
							delete(mp.pendingTxs, sender)
						}
						break
					}
				}
			}
		}
	}

	logx.Info("BlockCleanup completed", "removed_transactions", removedCount, "block_slot", block.Slot)
}

// This should be called periodically by the node to maintain mempool health
func (mp *Mempool) PeriodicCleanup() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	logx.Info("MEMPOOL", "Starting periodic mempool cleanup...")

	// Clean up stale transactions
	mp.cleanupStaleTransactions()

	// Promote any newly ready transactions
	// Clean up any outdated transactions and promote ready ones
	mp.cleanupOutdatedTransactions()

	// Log current mempool state
	totalPending := 0
	for _, pendingMap := range mp.pendingTxs {
		totalPending += len(pendingMap)
	}

	logx.Info("MEMPOOL", fmt.Sprintf(
		"Mempool cleanup complete - Ready: %d, Pending: %d, Total: %d\n",
		len(mp.readyQueue), totalPending, len(mp.txsBuf),
	))
}

func (mp *Mempool) cleanupOutdatedTransactions() {
	for sender, pendingMap := range mp.pendingTxs {
		account, err := mp.ledger.GetAccount(sender)
		if err != nil {
			logx.Error("MEMPOOL", "Error getting account for sender ", sender, ": ", err)
			continue
		}
		currentNonce := account.Nonce
		expectedNonce := currentNonce + 1

		// Remove any transactions with nonce <= current account nonce
		for nonce, pendingTx := range pendingMap {
			if nonce <= currentNonce {
				mp.removeTransaction(pendingTx.Tx)
				delete(pendingMap, nonce)
			}
		}

		// Clean up empty maps
		if len(pendingMap) == 0 {
			delete(mp.pendingTxs, sender)
			continue
		}

		// Promote ready transaction if it exists
		if pendingTx, exists := pendingMap[expectedNonce]; exists {
			mp.readyQueue = append(mp.readyQueue, pendingTx.Tx)
			delete(pendingMap, expectedNonce)

			if len(pendingMap) == 0 {
				delete(mp.pendingTxs, sender)
			}
		}
	}

	// Clean up ready queue of outdated transactions
	newReadyQueue := make([]*transaction.Transaction, 0, len(mp.readyQueue))
	for _, tx := range mp.readyQueue {
		account, err := mp.ledger.GetAccount(tx.Sender)
		if err != nil {
			// Skip this transaction if we can't get the account
			logx.Error("MEMPOOL", "Error getting account for sender ", tx.Sender, ": ", err)
			continue
		}
		currentNonce := account.Nonce
		if tx.Nonce > currentNonce {
			newReadyQueue = append(newReadyQueue, tx)
		} else {
			mp.removeTransaction(tx)
		}
	}
	mp.readyQueue = newReadyQueue
}

func (mp *Mempool) GetLargestReadyTransactionNonce(sender string) uint64 {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	if len(mp.readyQueue) == 0 {
		return 0
	}
	var largestNonce uint64 = 0
	// Check in ready queue
	for _, tx := range mp.readyQueue {
		if tx.Sender == sender {
			if tx.Nonce > largestNonce {
				largestNonce = tx.Nonce
			}
		}
	}
	return largestNonce
}

// GetLargestPendingNonce returns the largest nonce among pending transactions for a given sender
// Returns 0 if no pending transactions exist for the sender
func (mp *Mempool) GetLargestConsecutivePendingNonce(sender string, fromNonce uint64) uint64 {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	// Check in pending map
	pendingMap, exists := mp.pendingTxs[sender]
	if !exists || len(pendingMap) == 0 {
		return fromNonce
	}

	currentNonce := fromNonce
	for {
		if _, exists := pendingMap[currentNonce+1]; exists {
			currentNonce++
		} else {
			break
		}
	}

	return currentNonce
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
