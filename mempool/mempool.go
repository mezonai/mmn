package mempool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mezonai/mmn/errors"
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

	// Performance optimization: index map to avoid O(n) scans in ready queue
	readyQueueIndex  map[string]map[uint64]bool
	isMultisigWallet func(sender string) bool
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
		pendingTxs:      make(map[string]map[uint64]*PendingTransaction),
		readyQueue:      make([]*transaction.Transaction, 0),
		readyQueueIndex: make(map[string]map[uint64]bool),
		eventRouter:     eventRouter,
		txTracker:       txTracker,
		zkVerify:        zkVerify,
	}
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
		return "", errors.NewError(errors.ErrCodeDuplicateTransaction, errors.ErrMsgDuplicateTransaction)
	}

	// Check if mempool is full
	if len(mp.txsBuf) >= mp.max {
		mp.mu.RUnlock()
		logx.Error("MEMPOOL", "Dropping full mempool")
		monitoring.RecordRejectedTx(monitoring.TxMempoolFull)
		return "", errors.NewError(errors.ErrCodeMempoolFull, errors.ErrMsgMempoolFull)
	}
	mp.mu.RUnlock()

	// Now acquire write lock for validation and insertion
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Double-check after acquiring write lock (for race conditions)
	if _, exists := mp.txsBuf[txHash]; exists {
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping duplicate tx (double-check) %s", txHash))
		monitoring.RecordRejectedTx(monitoring.TxDuplicated)
		return "", errors.NewError(errors.ErrCodeDuplicateTransaction, errors.ErrMsgDuplicateTransaction)
	}

	if len(mp.txsBuf) >= mp.max {
		logx.Error("MEMPOOL", "Dropping full mempool (double-check)")
		monitoring.RecordRejectedTx(monitoring.TxMempoolFull)
		return "", errors.NewError(errors.ErrCodeMempoolFull, errors.ErrMsgMempoolFull)
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
	// based on ready queue and processing tracker
	largestReady := mp.GetLargestReadyTransactionNonce(tx.Sender)
	var largestProcessing uint64 = 0
	if mp.txTracker != nil {
		largestProcessing = mp.txTracker.GetLargestProcessingNonce(tx.Sender)
	}
	currentKnown := largestReady
	if largestProcessing > currentKnown {
		currentKnown = largestProcessing
	}

	// Fallback: if both ready and processing are 0 (empty mempool), get nonce from ledger
	if currentKnown == 0 && mp.ledger != nil {
		account, err := mp.ledger.GetAccount(tx.Sender)
		if err == nil && account != nil {
			currentKnown = account.Nonce
		}
	}

	isReady := tx.Nonce == currentKnown+1

	if isReady {
		// Add to ready queue for immediate processing
		mp.readyQueue = append(mp.readyQueue, tx)
		// Update index for O(1) lookup
		if mp.readyQueueIndex[tx.Sender] == nil {
			mp.readyQueueIndex[tx.Sender] = make(map[uint64]bool)
		}
		mp.readyQueueIndex[tx.Sender][tx.Nonce] = true
	} else {
		// Add to pending transactions
		if mp.pendingTxs[tx.Sender] == nil {
			mp.pendingTxs[tx.Sender] = make(map[uint64]*PendingTransaction)
		}
		mp.pendingTxs[tx.Sender][tx.Nonce] = &PendingTransaction{
			Tx:        tx,
			Timestamp: time.Now(),
		}
		logx.Debug("MEMPOOL", fmt.Sprintf("Added pending tx %s (sender: %s, nonce: %d, expected: %d)",
			txHash, tx.Sender[:8], tx.Nonce, currentKnown+1))
	}
}

func (mp *Mempool) validateBalance(tx *transaction.Transaction) error {
	if tx == nil {
		return errors.NewError(errors.ErrCodeInvalidTransaction, errors.ErrMsgInvalidTransaction)
	}
	if tx.Amount == nil {
		return errors.NewError(errors.ErrCodeInvalidAmount, errors.ErrMsgInvalidAmount)
	}

	if tx.Sender == tx.Recipient {
		return nil
	}

	senderAccount, err := mp.ledger.GetAccount(tx.Sender)
	if err != nil || senderAccount == nil {
		return errors.NewError(errors.ErrCodeAccountNotFound, errors.ErrMsgAccountNotFound)
	}

	// Add nil check for balance
	if senderAccount.Balance == nil {
		return errors.NewError(errors.ErrCodeAccountNotFound, errors.ErrMsgAccountNotFound)
	}

	availableBalance := new(uint256.Int).Set(senderAccount.Balance)

	// Subtract amounts from pending transactions to get true available balance
	if pendingNonces, exists := mp.pendingTxs[tx.Sender]; exists {
		for _, pendingTx := range pendingNonces {
			if pendingTx != nil && pendingTx.Tx != nil && pendingTx.Tx.Amount != nil && pendingTx.Tx.Nonce < tx.Nonce {
				availableBalance.Sub(availableBalance, pendingTx.Tx.Amount)
			}
		}
	}

	// Also subtract amounts from ready queue transactions for this sender
	for _, readyTx := range mp.readyQueue {
		if readyTx != nil && readyTx.Sender == tx.Sender && readyTx.Amount != nil && readyTx.Nonce < tx.Nonce {
			availableBalance.Sub(availableBalance, readyTx.Amount)
		}
	}

	if availableBalance.Cmp(tx.Amount) < 0 {
		return errors.NewError(errors.ErrCodeInsufficientFunds, errors.ErrMsgInsufficientFunds)
	}

	return nil
}

// Stateless validation, simple for tx
func (mp *Mempool) validateTransaction(tx *transaction.Transaction) error {

	if !tx.Verify(mp.zkVerify) {
		monitoring.RecordRejectedTx(monitoring.TxInvalidSignature)
		return errors.NewError(errors.ErrCodeInvalidSignature, errors.ErrMsgInvalidSignature)
	}

	// 2. Check for zero amount
	if tx.Amount == nil || tx.Amount.IsZero() {
		monitoring.RecordRejectedTx(monitoring.TxRejectedUnknown)
		return errors.NewError(errors.ErrCodeInvalidAmount, errors.ErrMsgInvalidAmount)
	}

	// 2.1. Check memo length (max 64 characters)
	if len(tx.TextData) > MAX_MEMO_CHARACTERS {
		return fmt.Errorf("memo too long: max %d chars, got %d", MAX_MEMO_CHARACTERS, len(tx.TextData))
	}

	// 3. Check sender account exists and get current state
	if mp.ledger == nil {
		monitoring.RecordRejectedTx(monitoring.TxRejectedUnknown)
		return errors.NewError(errors.ErrCodeInternal, errors.ErrMsgInternal)
	}

	senderAccount, err := mp.ledger.GetAccount(tx.Sender)
	if err != nil {
		monitoring.RecordRejectedTx(monitoring.TxRejectedUnknown)
		return errors.NewError(errors.ErrCodeAccountNotFound, errors.ErrMsgAccountNotFound)
	}
	if senderAccount == nil {
		monitoring.RecordRejectedTx(monitoring.TxSenderNotExist)
		return errors.NewError(errors.ErrCodeAccountNotFound, errors.ErrMsgAccountNotFound)
	}

	// 4. Enhanced nonce validation for zero-fee blockchain
	// Get current nonce (use cached value if available, otherwise from ledger)
	currentNonce := senderAccount.Nonce

	// Reject old transactions (nonce too low)
	if tx.Nonce <= currentNonce {
		monitoring.RecordRejectedTx(monitoring.TxInvalidNonce)
		return errors.NewError(errors.ErrCodeNonceTooLow, errors.ErrMsgNonceTooLow)
	}

	// 5. Prevent spam with reasonable future nonce limit
	if tx.Nonce > currentNonce+MaxFutureNonce {
		monitoring.RecordRejectedTx(monitoring.TxInvalidNonce)
		return errors.NewError(errors.ErrCodeNonceTooHigh, errors.ErrMsgNonceTooHigh)
	}

	// 6. Check pending transaction limits per sender
	if pendingNonces, exists := mp.pendingTxs[tx.Sender]; exists {
		if len(pendingNonces) >= MaxPendingPerSender {
			monitoring.RecordRejectedTx(monitoring.TxTooManyPending)
			return errors.NewError(errors.ErrCodeRateLimited, errors.ErrMsgRateLimited)
		}

		// 6.1. Check for duplicate nonce in pending transactions (redundant but safe)
		if _, nonceExists := pendingNonces[tx.Nonce]; nonceExists {
			monitoring.RecordRejectedTx(monitoring.TxInvalidNonce)
			return errors.NewError(errors.ErrCodeDuplicateTransaction, errors.ErrMsgDuplicateTransaction)
		}
	}

	// 7. Check for duplicate nonce in ready queue - O(1) with index
	if senderNonces, exists := mp.readyQueueIndex[tx.Sender]; exists {
		if senderNonces[tx.Nonce] {
			monitoring.RecordRejectedTx(monitoring.TxInvalidNonce)
			return errors.NewError(errors.ErrCodeDuplicateTransaction, errors.ErrMsgDuplicateTransaction)
		}
	}

	// 8. Validate balance accounting for existing pending/ready transactions
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

	batch := make([][]byte, 0, batchSize)
	processedCount := 0
	processedTxs := make([]*transaction.Transaction, 0, batchSize)

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
			processedTxs = append(processedTxs, tx)
			mp.removeTransaction(tx)
			processedCount++

			logx.Debug("MEMPOOL", fmt.Sprintf("Processed tx %s (sender: %s, nonce: %d)",
				txHash, tx.Sender[:8], tx.Nonce))
		}
		// Check if any pending transactions became ready after processing
		mp.promotePendingTransactions(readyTxs)
	}
	// Unlock before calling external components like txTracker
	mp.mu.Unlock()

	// Track transactions as processing outside of mempool lock
	if mp.txTracker != nil {
		for _, tx := range processedTxs {
			mp.txTracker.TrackProcessingTransaction(tx)
		}
	}

	logx.Info("MEMPOOL", fmt.Sprintf("PullBatch returning %d transactions", len(batch)))
	return batch
}

func (mp *Mempool) promotePendingTransactions(readyTxs []*transaction.Transaction) {
	for _, tx := range readyTxs {
		expectedNonce := tx.Nonce + 1
		if pendingMap, exists := mp.pendingTxs[tx.Sender]; exists {
			if pendingTx, hasNonce := pendingMap[expectedNonce]; hasNonce {
				// Move transaction from pending to ready queue
				mp.readyQueue = append(mp.readyQueue, pendingTx.Tx)
				// Update index
				if mp.readyQueueIndex[tx.Sender] == nil {
					mp.readyQueueIndex[tx.Sender] = make(map[uint64]bool)
				}
				mp.readyQueueIndex[tx.Sender][expectedNonce] = true
				delete(pendingMap, expectedNonce)

				// Cleanup empty pending maps
				if len(pendingMap) == 0 {
					delete(mp.pendingTxs, tx.Sender)
				}

				logx.Debug("MEMPOOL", fmt.Sprintf("Promoted pending tx for sender %s with nonce %d",
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
	_, exists := mp.txsBuf[txHash]
	return exists
}

// New method: GetTransaction - retrieve transaction data (read-only)
func (mp *Mempool) GetTransaction(txHash string) ([]byte, bool) {
	data, exists := mp.txsBuf[txHash]
	return data, exists
}

// New method: GetOrderedTransactions - get transactions in FIFO order (read-only)
func (mp *Mempool) GetOrderedTransactions() []string {
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
		// Remove from index
		if senderNonces, exists := mp.readyQueueIndex[tx.Sender]; exists {
			delete(senderNonces, tx.Nonce)
			if len(senderNonces) == 0 {
				delete(mp.readyQueueIndex, tx.Sender)
			}
		}
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
// NOTE: This is O(n) for txOrder removal. For batch removals, use removeTransactionBatch instead.
func (mp *Mempool) removeTransaction(tx *transaction.Transaction) {
	txHash := tx.Hash()
	delete(mp.txsBuf, txHash)
	monitoring.SetMempoolSize(mp.Size())

	// O(n) linear search - prefer batch removal when removing multiple txs
	for i, hash := range mp.txOrder {
		if hash == txHash {
			mp.txOrder = append(mp.txOrder[:i], mp.txOrder[i+1:]...)
			break
		}
	}
}

// removeTransactionBatch removes multiple transactions efficiently using a hash set
// This is O(n) instead of O(n*m) where n=txOrder length, m=number of txs to remove
func (mp *Mempool) removeTransactionBatch(txs []*transaction.Transaction) {
	if len(txs) == 0 {
		return
	}

	// Build hash set for O(1) lookup
	txHashSet := make(map[string]bool, len(txs))
	for _, tx := range txs {
		txHash := tx.Hash()
		txHashSet[txHash] = true
		delete(mp.txsBuf, txHash)
	}

	// Single pass filter - O(n) instead of O(n*m)
	newTxOrder := make([]string, 0, len(mp.txOrder))
	for _, hash := range mp.txOrder {
		if !txHashSet[hash] {
			newTxOrder = append(newTxOrder, hash)
		}
	}
	mp.txOrder = newTxOrder
	monitoring.SetMempoolSize(mp.Size())
}

// cleanupStaleTransactions removes transactions that have been pending too long
func (mp *Mempool) cleanupStaleTransactions() {
	now := time.Now()
	staleThreshold := now.Add(-StaleTimeout)

	// Collect all stale transactions for batch removal
	staleTxs := make([]*transaction.Transaction, 0)

	for sender, pendingMap := range mp.pendingTxs {
		for nonce, pendingTx := range pendingMap {
			if pendingTx.Timestamp.Before(staleThreshold) {
				staleTxs = append(staleTxs, pendingTx.Tx)
				delete(pendingMap, nonce)
				logx.Debug("MEMPOOL", fmt.Sprintf("Removed stale transaction (sender: %s, nonce: %d)",
					sender[:8], nonce))
			}
		}
		if len(pendingMap) == 0 {
			delete(mp.pendingTxs, sender)
		}
	}

	// Batch remove from txsBuf and txOrder - O(n) instead of O(n*m)
	if len(staleTxs) > 0 {
		mp.removeTransactionBatch(staleTxs)
		logx.Info("MEMPOOL", fmt.Sprintf("Batch removed %d stale transactions", len(staleTxs)))
	}
}

func (mp *Mempool) BlockCleanup(block *block.BroadcastedBlock) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Track removed transactions for logging
	removedCount := 0

	// Pre-build hash set for O(1) lookup instead of O(n) search
	txHashSet := make(map[string]bool)
	for _, entry := range block.Entries {
		for _, tx := range entry.Transactions {
			txHashSet[tx.Hash()] = true
		}
	}

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
				removedCount++
			}
		}
	}

	// Bulk update monitoring after all removals
	if removedCount > 0 {
		monitoring.SetMempoolSize(mp.Size())
	}

	// Remove from txOrder - use filtering instead of repeated slice operations
	newTxOrder := make([]string, 0, len(mp.txOrder))
	for _, hash := range mp.txOrder {
		if !txHashSet[hash] {
			newTxOrder = append(newTxOrder, hash)
		}
	}
	mp.txOrder = newTxOrder

	// Remove from ready queue - use filtering instead of repeated slice operations
	newReadyQueue := make([]*transaction.Transaction, 0, len(mp.readyQueue))
	for _, tx := range mp.readyQueue {
		if !txHashSet[tx.Hash()] {
			newReadyQueue = append(newReadyQueue, tx)
		} else {
			// Remove from index
			if senderNonces, exists := mp.readyQueueIndex[tx.Sender]; exists {
				delete(senderNonces, tx.Nonce)
				if len(senderNonces) == 0 {
					delete(mp.readyQueueIndex, tx.Sender)
				}
			}
		}
	}
	mp.readyQueue = newReadyQueue

	// Remove from pending transactions - optimize by using sender lookup from transactions
	for _, entry := range block.Entries {
		for _, tx := range entry.Transactions {
			if nonceTxs, exists := mp.pendingTxs[tx.Sender]; exists {
				delete(nonceTxs, tx.Nonce)
				// Clean up empty sender map
				if len(nonceTxs) == 0 {
					delete(mp.pendingTxs, tx.Sender)
				}
			}
		}
	}

	logx.Info("BlockCleanup completed", "removed_transactions", removedCount, "block_slot", block.Slot)
}

// PeriodicCleanup should be called periodically by the node to maintain mempool health
func (mp *Mempool) PeriodicCleanup() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	logx.Info("MEMPOOL", "Starting periodic mempool cleanup...")

	// Clean up stale transactions
	mp.cleanupStaleTransactions()

	// Promote any newly ready transactions
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
	// Optimization: Batch collect all unique senders first to minimize repeated DB calls
	uniqueSenders := make([]string, 0, len(mp.pendingTxs)+len(mp.readyQueue))
	senderSet := make(map[string]bool)
	for sender := range mp.pendingTxs {
		if !senderSet[sender] {
			uniqueSenders = append(uniqueSenders, sender)
			senderSet[sender] = true
		}
	}
	for _, tx := range mp.readyQueue {
		if !senderSet[tx.Sender] {
			uniqueSenders = append(uniqueSenders, tx.Sender)
			senderSet[tx.Sender] = true
		}
	}

	// Batch get all account states - SINGLE CGO CALL instead of N calls!
	accounts, err := mp.ledger.GetAccountBatch(uniqueSenders)
	if err != nil {
		logx.Error("MEMPOOL", "Error batch getting accounts: ", err)
		return
	}

	// Build nonce cache from batch results
	senderNonceCache := make(map[string]uint64, len(accounts))
	for addr, account := range accounts {
		if account != nil {
			senderNonceCache[addr] = account.Nonce
		}
	}

	// Collect outdated transactions for batch removal
	outdatedTxs := make([]*transaction.Transaction, 0)

	// Process pending transactions with cached nonces
	for sender, pendingMap := range mp.pendingTxs {
		currentNonce, exists := senderNonceCache[sender]
		if !exists {
			continue
		}
		expectedNonce := currentNonce + 1

		// Collect transactions with nonce <= current account nonce
		for nonce, pendingTx := range pendingMap {
			if nonce <= currentNonce {
				outdatedTxs = append(outdatedTxs, pendingTx.Tx)
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
			// Update index
			if mp.readyQueueIndex[sender] == nil {
				mp.readyQueueIndex[sender] = make(map[uint64]bool)
			}
			mp.readyQueueIndex[sender][expectedNonce] = true
			delete(pendingMap, expectedNonce)

			if len(pendingMap) == 0 {
				delete(mp.pendingTxs, sender)
			}
		}
	}

	// Clean up ready queue of outdated transactions with cached nonces
	newReadyQueue := make([]*transaction.Transaction, 0, len(mp.readyQueue))
	for _, tx := range mp.readyQueue {
		currentNonce, exists := senderNonceCache[tx.Sender]
		if !exists {
			// Skip if we couldn't get account
			continue
		}
		if tx.Nonce > currentNonce {
			newReadyQueue = append(newReadyQueue, tx)
		} else {
			// Collect for batch removal
			outdatedTxs = append(outdatedTxs, tx)
			// Remove from index
			if senderNonces, idxExists := mp.readyQueueIndex[tx.Sender]; idxExists {
				delete(senderNonces, tx.Nonce)
				if len(senderNonces) == 0 {
					delete(mp.readyQueueIndex, tx.Sender)
				}
			}
		}
	}
	mp.readyQueue = newReadyQueue

	// Batch remove all outdated transactions - O(n) instead of O(n*m)
	if len(outdatedTxs) > 0 {
		mp.removeTransactionBatch(outdatedTxs)
		logx.Info("MEMPOOL", fmt.Sprintf("Batch removed %d outdated transactions", len(outdatedTxs)))
	}
}

func (mp *Mempool) GetLargestReadyTransactionNonce(sender string) uint64 {
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

func (mp *Mempool) SetIsMultisigWallet(isMultisigWallet func(sender string) bool) {
	mp.isMultisigWallet = isMultisigWallet
}

// GetMempoolStats returns current mempool statistics
func (mp *Mempool) GetMempoolStats() map[string]interface{} {
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
