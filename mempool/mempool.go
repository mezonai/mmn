package mempool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/logx"

	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/transaction"
)

// Constants for zero-fee blockchain optimization
const (
	MaxPendingPerSender = 60               // Max future transactions per sender
	StaleTimeout        = 60 * time.Minute // Remove old pending transactions
	MaxFutureNonce      = 64               // Max nonce distance from current
)

// PendingTransaction wraps a transaction with timestamp for timeout management
type PendingTransaction struct {
	Tx        *transaction.Transaction
	Timestamp time.Time
}

// CachedAccountState caches account state for faster validation
type CachedAccountState struct {
	Balance     *uint256.Int
	Nonce       uint64
	LastUpdated time.Time
}

type Mempool struct {
	// Global lock for batch operations and consistency
	mu sync.RWMutex

	// Sharded locks for better concurrency
	shardMutexes []sync.RWMutex
	shardCount   int

	// Sharded transaction storage
	shardTxsBuf  []map[string][]byte
	shardTxOrder []map[string]int // hash -> index for O(1) removal

	max         int
	broadcaster interfaces.Broadcaster
	ledger      interfaces.Ledger

	pendingTxs  map[string]map[uint64]*PendingTransaction // sender -> nonce -> pending tx
	readyQueue  []*transaction.Transaction                // ready-to-process transactions
	eventRouter *events.EventRouter                       // Event router for transaction status updates
	txTracker   interfaces.TransactionTrackerInterface    // Transaction state tracker

	// Account state cache for faster validation
	accountCache map[string]*CachedAccountState
	cacheMutex   sync.RWMutex
}

func NewMempool(max int, broadcaster interfaces.Broadcaster, ledger interfaces.Ledger, eventRouter *events.EventRouter, txTracker interfaces.TransactionTrackerInterface) *Mempool {
	shardCount := 16 // Use 16 shards for better concurrency

	// Initialize sharded storage
	shardMutexes := make([]sync.RWMutex, shardCount)
	shardTxsBuf := make([]map[string][]byte, shardCount)
	shardTxOrder := make([]map[string]int, shardCount)

	for i := 0; i < shardCount; i++ {
		shardTxsBuf[i] = make(map[string][]byte, max/shardCount)
		shardTxOrder[i] = make(map[string]int)
	}

	return &Mempool{
		mu:           sync.RWMutex{},
		shardMutexes: shardMutexes,
		shardCount:   shardCount,
		shardTxsBuf:  shardTxsBuf,
		shardTxOrder: shardTxOrder,
		max:          max,
		broadcaster:  broadcaster,
		ledger:       ledger,

		// Initialize zero-fee optimization fields
		pendingTxs:   make(map[string]map[uint64]*PendingTransaction),
		readyQueue:   make([]*transaction.Transaction, 0),
		eventRouter:  eventRouter,
		txTracker:    txTracker,
		accountCache: make(map[string]*CachedAccountState),
	}
}

// getShard returns the shard index for a given transaction hash
func (mp *Mempool) getShard(txHash string) int {
	hash := 0
	for _, c := range txHash {
		hash = hash*31 + int(c)
	}
	if hash < 0 {
		hash = -hash
	}
	return hash % mp.shardCount
}

// getCachedAccountState returns cached account state or fetches from ledger
func (mp *Mempool) getCachedAccountState(sender string) (*CachedAccountState, error) {
	// Check cache first
	mp.cacheMutex.RLock()
	if cached, exists := mp.accountCache[sender]; exists {
		if time.Since(cached.LastUpdated) < 5*time.Second { // Cache for 5 seconds
			mp.cacheMutex.RUnlock()
			return cached, nil
		}
	}
	mp.cacheMutex.RUnlock()

	// Fetch from ledger
	account, err := mp.ledger.GetAccount(sender)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, fmt.Errorf("account not found")
	}

	// Cache the result
	cached := &CachedAccountState{
		Balance:     new(uint256.Int).Set(account.Balance),
		Nonce:       account.Nonce,
		LastUpdated: time.Now(),
	}

	mp.cacheMutex.Lock()
	mp.accountCache[sender] = cached
	mp.cacheMutex.Unlock()

	return cached, nil
}

// validateTransactionOptimized performs optimized validation with caching
func (mp *Mempool) validateTransactionOptimized(tx *transaction.Transaction) error {
	// 1. Verify signature
	if !tx.Verify() {
		return fmt.Errorf("invalid signature")
	}

	// 2. Check for zero amount
	if tx.Amount == nil || tx.Amount.IsZero() {
		return fmt.Errorf("zero amount not allowed")
	}

	// 3. Get cached account state
	accountState, err := mp.getCachedAccountState(tx.Sender)
	if err != nil {
		return fmt.Errorf("could not get sender account: %w", err)
	}

	// 4. Enhanced nonce validation
	currentNonce := accountState.Nonce

	// Reject old transactions (nonce too low)
	if tx.Nonce <= currentNonce {
		return fmt.Errorf("nonce too low: expected > %d, got %d", currentNonce, tx.Nonce)
	}

	// Prevent spam with reasonable future nonce limit
	if tx.Nonce > currentNonce+MaxFutureNonce {
		return fmt.Errorf("nonce too high: max allowed %d, got %d",
			currentNonce+MaxFutureNonce, tx.Nonce)
	}

	// 5. Validate balance using cached state
	if accountState.Balance.Cmp(tx.Amount) < 0 {
		return fmt.Errorf("insufficient balance: have %s, need %s",
			accountState.Balance.String(), tx.Amount.String())
	}

	return nil
}

// validateBatchTransactions validates multiple transactions in batch for better performance
func (mp *Mempool) validateBatchTransactions(transactions []*transaction.Transaction) []error {
	errors := make([]error, len(transactions))

	// Group transactions by sender for batch account state retrieval
	senderGroups := make(map[string][]int)
	for i, tx := range transactions {
		senderGroups[tx.Sender] = append(senderGroups[tx.Sender], i)
	}

	// Validate each sender's transactions
	for sender, indices := range senderGroups {
		accountState, err := mp.getCachedAccountState(sender)
		if err != nil {
			// Set error for all transactions from this sender
			for _, index := range indices {
				errors[index] = fmt.Errorf("failed to get account state for %s: %v", sender, err)
			}
			continue
		}

		// Validate each transaction from this sender
		for _, index := range indices {
			tx := transactions[index]

			// Check signature
			if !tx.Verify() {
				errors[index] = fmt.Errorf("invalid signature")
				continue
			}

			// Check for zero amount
			if tx.Amount == nil || tx.Amount.IsZero() {
				errors[index] = fmt.Errorf("zero amount not allowed")
				continue
			}

			// Check nonce
			if tx.Nonce <= accountState.Nonce {
				errors[index] = fmt.Errorf("invalid nonce: expected > %d, got %d", accountState.Nonce, tx.Nonce)
				continue
			}

			// Check balance
			if accountState.Balance.Cmp(tx.Amount) < 0 {
				errors[index] = fmt.Errorf("insufficient balance: have %s, need %s", accountState.Balance.String(), tx.Amount.String())
				continue
			}
		}
	}

	return errors
}

func (mp *Mempool) AddTx(tx *transaction.Transaction, broadcast bool) (string, error) {
	// Generate hash first (read-only operation)
	txHash := tx.Hash()
	shardIndex := mp.getShard(txHash)

	// Quick check for duplicate using shard read lock
	mp.shardMutexes[shardIndex].RLock()
	if _, exists := mp.shardTxsBuf[shardIndex][txHash]; exists {
		mp.shardMutexes[shardIndex].RUnlock()
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping duplicate tx %s", txHash))
		return "", fmt.Errorf("duplicate transaction")
	}

	// Check if mempool is full (approximate check)
	totalSize := 0
	for i := 0; i < mp.shardCount; i++ {
		totalSize += len(mp.shardTxsBuf[i])
	}
	if totalSize >= mp.max {
		mp.shardMutexes[shardIndex].RUnlock()
		logx.Error("MEMPOOL", "Dropping full mempool")
		return "", fmt.Errorf("mempool full")
	}
	mp.shardMutexes[shardIndex].RUnlock()

	// Validate transaction first (before acquiring locks)
	if err := mp.validateTransactionOptimized(tx); err != nil {
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping invalid tx %s: %v", txHash, err))
		return "", err
	}

	// Now acquire shard write lock for insertion
	mp.shardMutexes[shardIndex].Lock()
	defer mp.shardMutexes[shardIndex].Unlock()

	// Double-check after acquiring write lock (for race conditions)
	if _, exists := mp.shardTxsBuf[shardIndex][txHash]; exists {
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping duplicate tx (double-check) %s", txHash))
		return "", fmt.Errorf("duplicate transaction")
	}

	logx.Info("MEMPOOL", fmt.Sprintf("Adding tx %+v", tx))

	// Determine if transaction is ready or pending
	mp.processTransactionToQueue(tx)

	// Add to sharded storage
	mp.shardTxsBuf[shardIndex][txHash] = tx.Bytes()
	mp.shardTxOrder[shardIndex][txHash] = len(mp.shardTxsBuf[shardIndex])

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
				logx.Error("MEMPOOL", fmt.Sprintf("Broadcast error: %v", err))
			}
		}()
	}

	logx.Info("MEMPOOL", fmt.Sprintf("Added tx %s", txHash))
	return txHash, nil
}

func (mp *Mempool) processTransactionToQueue(tx *transaction.Transaction) {
	txHash := tx.Hash()
	accountState, err := mp.getCachedAccountState(tx.Sender)
	if err != nil {
		logx.Error("MEMPOOL", fmt.Sprintf("could not get account: %v", err))
		return // Handle error appropriately based on context
	}
	currentNonce := accountState.Nonce
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
	senderAccount, err := mp.ledger.GetAccount(tx.Sender)
	if err != nil || senderAccount == nil {
		return fmt.Errorf("could not get sender accont: %w", err)
	}
	availableBalance := new(uint256.Int).Set(senderAccount.Balance)

	// Subtract amounts from pending transactions to get true available balance
	if pendingNonces, exists := mp.pendingTxs[tx.Sender]; exists {
		for _, pendingTx := range pendingNonces {
			availableBalance.Sub(availableBalance, pendingTx.Tx.Amount)
		}
	}

	// Also subtract amounts from ready queue transactions for this sender
	for _, readyTx := range mp.readyQueue {
		if readyTx.Sender == tx.Sender {
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
	if !tx.Verify() {
		return fmt.Errorf("invalid signature")
	}

	// 2. Check for zero amount
	if tx.Amount == nil || tx.Amount.IsZero() {
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
	currentNonce := senderAccount.Nonce

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

	// 9. Validate balance accounting for existing pending/ready transactions
	if err := mp.validateBalance(tx); err != nil {
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping tx %s due to insufficient balance: %s", tx.Hash(), err.Error()))
		return err
	}

	return nil
}

// PullBatch implements smart dependency resolution for zero-fee blockchain
func (mp *Mempool) PullBatch(batchSize int) [][]byte {
	// Use global lock for batch processing to maintain consistency
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

// Size returns total number of transactions across all shards
func (mp *Mempool) Size() int {
	total := 0
	for i := 0; i < mp.shardCount; i++ {
		mp.shardMutexes[i].RLock()
		total += len(mp.shardTxsBuf[i])
		mp.shardMutexes[i].RUnlock()
	}
	return total
}

// New method: GetTransactionCount - read-only operation
func (mp *Mempool) GetTransactionCount() int {
	return mp.Size()
}

// New method: HasTransaction - check if transaction exists (read-only)
func (mp *Mempool) HasTransaction(txHash string) bool {
	shardIndex := mp.getShard(txHash)
	mp.shardMutexes[shardIndex].RLock()
	defer mp.shardMutexes[shardIndex].RUnlock()
	_, exists := mp.shardTxsBuf[shardIndex][txHash]
	return exists
}

// New method: GetTransaction - retrieve transaction data (read-only)
func (mp *Mempool) GetTransaction(txHash string) ([]byte, bool) {
	shardIndex := mp.getShard(txHash)
	mp.shardMutexes[shardIndex].RLock()
	defer mp.shardMutexes[shardIndex].RUnlock()
	data, exists := mp.shardTxsBuf[shardIndex][txHash]
	return data, exists
}

// New method: GetOrderedTransactions - get transactions in FIFO order (read-only)
func (mp *Mempool) GetOrderedTransactions() []string {
	// For sharded mempool, we don't maintain global order
	// Return empty slice for compatibility
	return []string{}
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
	shardIndex := mp.getShard(txHash)

	// Remove from sharded storage
	mp.shardMutexes[shardIndex].Lock()
	delete(mp.shardTxsBuf[shardIndex], txHash)
	delete(mp.shardTxOrder[shardIndex], txHash)
	mp.shardMutexes[shardIndex].Unlock()
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

func (mp *Mempool) BlockCleanup(block *block.Block) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Track removed transactions for logging
	removedCount := 0

	// Iterate through all entries in the block and clean up all transaction references
	for _, entry := range block.Entries {
		for _, txHash := range entry.TxHashes {
			// Remove from sharded storage
			shardIndex := mp.getShard(txHash)
			mp.shardMutexes[shardIndex].Lock()
			if _, exists := mp.shardTxsBuf[shardIndex][txHash]; exists {
				delete(mp.shardTxsBuf[shardIndex], txHash)
				delete(mp.shardTxOrder[shardIndex], txHash)
				removedCount++
			}
			mp.shardMutexes[shardIndex].Unlock()

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

	// Calculate total transactions across all shards
	totalTransactions := 0
	for i := 0; i < mp.shardCount; i++ {
		mp.shardMutexes[i].RLock()
		totalTransactions += len(mp.shardTxsBuf[i])
		mp.shardMutexes[i].RUnlock()
	}

	logx.Info("MEMPOOL", fmt.Sprintf(
		"Mempool cleanup complete - Ready: %d, Pending: %d, Total: %d\n",
		len(mp.readyQueue), totalPending, totalTransactions,
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

// GetLargestPendingNonce returns the largest nonce among pending transactions for a given sender
// Returns 0 if no pending transactions exist for the sender
func (mp *Mempool) GetLargestPendingNonce(sender string) uint64 {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	var largestNonce uint64 = 0
	// Check in pending map
	pendingMap, exists := mp.pendingTxs[sender]
	if exists && len(pendingMap) > 0 {
		for nonce := range pendingMap {
			if nonce > largestNonce {
				largestNonce = nonce
			}
		}
	}

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

// AddBatchTx adds multiple transactions in batch for better performance
func (mp *Mempool) AddBatchTx(transactions []*transaction.Transaction, broadcast bool) ([]string, []error) {
	if len(transactions) == 0 {
		return nil, nil
	}

	// Use global lock for batch operations to maintain consistency
	mp.mu.Lock()
	defer mp.mu.Unlock()

	results := make([]string, len(transactions))
	errors := make([]error, len(transactions))

	// Validate all transactions in batch first
	validationErrors := mp.validateBatchTransactions(transactions)

	// Process valid transactions
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, mp.shardCount) // Limit concurrent operations

	for i, tx := range transactions {
		// Skip invalid transactions
		if validationErrors[i] != nil {
			errors[i] = validationErrors[i]
			continue
		}

		wg.Add(1)
		go func(index int, transaction *transaction.Transaction) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire semaphore
			defer func() { <-semaphore }() // Release semaphore

			// Add to sharded storage
			txHash := transaction.Hash()
			shardIndex := mp.getShard(txHash)

			mp.shardMutexes[shardIndex].Lock()
			mp.shardTxsBuf[shardIndex][txHash] = transaction.Serialize()
			mp.shardTxOrder[shardIndex][txHash] = len(mp.shardTxsBuf[shardIndex])
			mp.shardMutexes[shardIndex].Unlock()

			// Process to queue
			mp.processTransactionToQueue(transaction)

			// Broadcast if requested
			if broadcast {
				ctx := context.Background()
				if err := mp.broadcaster.TxBroadcast(ctx, transaction); err != nil {
					logx.Error("MEMPOOL", "Failed to broadcast transaction:", err)
				}
			}

			results[index] = txHash
		}(i, tx)
	}

	wg.Wait()
	return results, errors
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

	// Calculate total transactions across all shards
	totalTransactions := 0
	for i := 0; i < mp.shardCount; i++ {
		mp.shardMutexes[i].RLock()
		totalTransactions += len(mp.shardTxsBuf[i])
		mp.shardMutexes[i].RUnlock()
	}

	return map[string]interface{}{
		"ready_transactions":   len(mp.readyQueue),
		"pending_transactions": totalPending,
		"total_transactions":   totalTransactions,
		"pending_by_sender":    pendingBySender,
		"unique_senders":       len(mp.pendingTxs),
		"shard_count":          mp.shardCount,
		"cache_size":           len(mp.accountCache),
	}
}
