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
	"github.com/mezonai/mmn/logx"

	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/transaction"
)

const (
	NONCE_WINDOW = 150
)

type Mempool struct {
	mu              sync.RWMutex // Changed to RWMutex for better concurrency
	txs             map[string]*transaction.Transaction
	txOrder         []string                       // Maintain FIFO order
	dedupTxHashSet  map[string]struct{}            // Set for deduplication
	senderTxHashSet map[string]map[string]struct{} // sender -> txHash
	max             int
	netCurrentSlot  uint64
	broadcaster     interfaces.Broadcaster
	ledger          interfaces.Ledger

	dedupService *DedupService
	eventRouter  *events.EventRouter                    // Event router for transaction status updates
	txTracker    interfaces.TransactionTrackerInterface // Transaction state tracker
	zkVerify     *zkverify.ZkVerify                     // Zk verify for zk transactions
}

func NewMempool(max int, broadcaster interfaces.Broadcaster, ledger interfaces.Ledger, dedupService *DedupService, eventRouter *events.EventRouter,
	txTracker interfaces.TransactionTrackerInterface, zkVerify *zkverify.ZkVerify) *Mempool {
	return &Mempool{
		txs:             make(map[string]*transaction.Transaction, max),
		txOrder:         make([]string, 0, max),
		dedupTxHashSet:  make(map[string]struct{}),
		senderTxHashSet: make(map[string]map[string]struct{}),
		max:             max,
		netCurrentSlot:  0,
		broadcaster:     broadcaster,
		ledger:          ledger,

		dedupService: dedupService,
		eventRouter:  eventRouter,
		txTracker:    txTracker,
		zkVerify:     zkVerify,
	}
}

func (mp *Mempool) AddTx(tx *transaction.Transaction, broadcast bool) (string, error) {
	// Generate hash first (read-only operation)
	txHash := tx.Hash()
	txDedupHash := tx.DedupHash()
	monitoring.IncreaseReceivedTxCount()

	// Quick check for duplicate using read lock
	mp.mu.RLock()
	if _, exists := mp.dedupTxHashSet[txDedupHash]; exists {
		mp.mu.RUnlock()
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping duplicate tx %s", txHash))
		monitoring.RecordRejectedTx(monitoring.TxDuplicated)
		return "", errors.NewError(errors.ErrCodeDuplicateTransaction, errors.ErrMsgDuplicateTransaction)
	}

	// Check if mempool is full
	if len(mp.txs) >= mp.max {
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
	if _, exists := mp.dedupTxHashSet[txDedupHash]; exists {
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping duplicate tx (double-check) %s", txHash))
		monitoring.RecordRejectedTx(monitoring.TxDuplicated)
		return "", errors.NewError(errors.ErrCodeDuplicateTransaction, errors.ErrMsgDuplicateTransaction)
	}

	if len(mp.txs) >= mp.max {
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

	// Always add to txs and txOrder for compatibility
	mp.txs[txHash] = tx
	monitoring.SetMempoolSize(mp.Size())
	mp.txOrder = append(mp.txOrder, txHash)
	mp.dedupTxHashSet[txDedupHash] = struct{}{}

	// Add to mapSenderTxs
	sender := tx.Sender
	if _, exists := mp.senderTxHashSet[sender]; !exists {
		mp.senderTxHashSet[sender] = make(map[string]struct{})
	}
	mp.senderTxHashSet[sender][txHash] = struct{}{}

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

	// Subtract amounts from existing mempool transactions from the same sender
	if txHashes, exists := mp.senderTxHashSet[tx.Sender]; exists {
		for txHash := range txHashes {
			if tx, exists := mp.txs[txHash]; exists {
				availableBalance.Sub(availableBalance, tx.Amount)
			}
		}
	}

	if availableBalance.Cmp(tx.Amount) < 0 {
		return errors.NewError(errors.ErrCodeInsufficientFunds, errors.ErrMsgInsufficientFunds)
	}

	return nil
}

func (mp *Mempool) validateNonce(txs []*transaction.Transaction) error {
	minNonce := uint64(1)
	if mp.netCurrentSlot > NONCE_WINDOW {
		minNonce = mp.netCurrentSlot - NONCE_WINDOW
	}
	for _, tx := range txs {
		if tx.Nonce < minNonce {
			return errors.NewError(errors.ErrCodeNonceTooLow, errors.ErrMsgNonceTooLow)
		}
		if tx.Nonce > mp.netCurrentSlot {
			return errors.NewError(errors.ErrCodeNonceTooHigh, errors.ErrMsgNonceTooHigh)
		}
	}
	return nil
}

func (mp *Mempool) validateDuplicateTxs(txs []*transaction.Transaction) error {
	for _, tx := range txs {
		if mp.dedupService.IsDuplicate(tx.DedupHash()) {
			return errors.NewError(errors.ErrCodeDuplicateTransaction, errors.ErrMsgDuplicateTransaction)
		}
	}
	return nil
}

// Stateless validation, simple for tx
func (mp *Mempool) validateTransaction(tx *transaction.Transaction) error {
	// 1. Verify signature
	if !tx.Verify(mp.zkVerify) {
		monitoring.RecordRejectedTx(monitoring.TxInvalidSignature)
		return errors.NewError(errors.ErrCodeInvalidSignature, errors.ErrMsgInvalidSignature)
	}

	// 2. Check for zero amount
	if tx.Amount == nil || tx.Amount.IsZero() {
		monitoring.RecordRejectedTx(monitoring.TxRejectedUnknown)
		return errors.NewError(errors.ErrCodeInvalidAmount, errors.ErrMsgInvalidAmount)
	}

	// 3. Check memo length (max 64 characters)
	if len(tx.TextData) > MAX_MEMO_CHARACTORS {
		monitoring.RecordRejectedTx(monitoring.TxRejectedUnknown)
		return fmt.Errorf("memo too long: max %d chars, got %d", MAX_MEMO_CHARACTORS, len(tx.TextData))
	}

	senderAccount, err := mp.ledger.GetAccount(tx.Sender)
	if err != nil || senderAccount == nil {
		monitoring.RecordRejectedTx(monitoring.TxRejectedUnknown)
		return errors.NewError(errors.ErrCodeAccountNotFound, errors.ErrMsgAccountNotFound)
	}

	// 4. Check nonce
	if err := mp.validateNonce([]*transaction.Transaction{tx}); err != nil {
		monitoring.RecordRejectedTx(monitoring.TxInvalidNonce)
		return err
	}
	// 5. Check duplicate transaction
	if err := mp.validateDuplicateTxs([]*transaction.Transaction{tx}); err != nil {
		monitoring.RecordRejectedTx(monitoring.TxDuplicated)
		return err
	}

	// 6. Validate balance accounting
	if err := mp.validateBalance(tx); err != nil {
		monitoring.RecordRejectedTx(monitoring.TxInsufficientBalance)
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping tx %s due to insufficient balance: %s", tx.Hash(), err.Error()))
		return err
	}

	return nil
}

// PullBatch implements smart dependency resolution for zero-fee blockchain
func (mp *Mempool) PullBatch(slot uint64, batchSize int) []*transaction.Transaction {
	mp.mu.Lock()

	n := min(batchSize, len(mp.txOrder))

	selected := mp.txOrder[:n]
	mp.txOrder = mp.txOrder[n:]

	result := make([]*transaction.Transaction, 0, n)
	dedupTxHashes := make([]string, 0, n)

	for _, txHash := range selected {
		tx, exists := mp.txs[txHash]
		if !exists {
			continue
		}

		result = append(result, tx)
		dedupTxHashes = append(dedupTxHashes, tx.DedupHash())

		// Remove from main buffer + dedup set
		delete(mp.txs, txHash)
		delete(mp.dedupTxHashSet, tx.DedupHash())

		// Remove from mapSenderTxs
		if senderMap, ok := mp.senderTxHashSet[tx.Sender]; ok {
			delete(senderMap, txHash)
			if len(senderMap) == 0 {
				delete(mp.senderTxHashSet, tx.Sender)
			}
		}
	}

	monitoring.SetMempoolSize(mp.Size())
	mp.mu.Unlock()

	mp.dedupService.Add(slot, dedupTxHashes)

	// Track outside lock
	if mp.txTracker != nil && len(result) > 0 {
		for _, tx := range result {
			mp.txTracker.TrackProcessingTransaction(tx)
		}
	}

	return result
}

// Size returns current size of mempool without locking
func (mp *Mempool) Size() int {
	return len(mp.txs)
}

// New method: GetTransactionCount - read-only operation
func (mp *Mempool) GetTransactionCount() int {
	return mp.Size()
}

// New method: GetTransaction - retrieve transaction data (read-only)
func (mp *Mempool) GetTransaction(txHash string) (*transaction.Transaction, bool) {
	data, exists := mp.txs[txHash]
	return data, exists
}

// New method: GetOrderedTransactions - get transactions in FIFO order (read-only)
func (mp *Mempool) GetOrderedTransactions() []string {
	// Return a copy to avoid external modification
	result := make([]string, len(mp.txOrder))
	copy(result, mp.txOrder)
	return result
}

func (mp *Mempool) GetCurrentSlot() uint64 {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.netCurrentSlot
}

func (mp *Mempool) SetCurrentSlot(slot uint64) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.netCurrentSlot = slot
}

func (mp *Mempool) VerifyBlockTransactions(txs []*transaction.Transaction) error {
	if err := mp.validateNonce(txs); err != nil {
		return err
	}

	if err := mp.validateDuplicateTxs(txs); err != nil {
		return err
	}

	return nil
}

func (mp *Mempool) BlockCleanup(slot uint64, txHashSet map[string]struct{}) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	for txHash := range txHashSet {
		if tx, exists := mp.txs[txHash]; exists {
			delete(mp.dedupTxHashSet, tx.DedupHash())
			delete(mp.txs, txHash)
		}
	}

	newTxOrder := make([]string, 0, len(mp.txOrder))
	for _, hash := range mp.txOrder {
		if _, shouldRemove := txHashSet[hash]; shouldRemove {
			continue
		}
		newTxOrder = append(newTxOrder, hash)
	}
	mp.txOrder = newTxOrder

	removedCount := 0
	for sender, txSet := range mp.senderTxHashSet {
		if removedCount >= len(txSet) {
			break
		}

		for txHash := range txSet {
			if _, shouldRemove := txHashSet[txHash]; shouldRemove {
				delete(txSet, txHash)
				removedCount++
			}
		}
		if len(txSet) == 0 {
			delete(mp.senderTxHashSet, sender)
		}
	}

	// Update monitoring
	if removedCount > 0 {
		monitoring.SetMempoolSize(mp.Size())
	}

	logx.Info("MEMPOOL", fmt.Sprintf("BlockCleanup done — removed %d tx(s) from mempool for slot %d", removedCount, slot))
}
