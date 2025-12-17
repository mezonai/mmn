package mempool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mezonai/mmn/errors"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/monitoring"
	"github.com/mezonai/mmn/security/validation"
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/types"
	"github.com/mezonai/mmn/zkverify"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/logx"

	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/transaction"
)

const (
	NonceWindow = 150
)

type Mempool struct {
	mu                sync.RWMutex // Changed to RWMutex for better concurrency
	txs               map[string]*transaction.Transaction
	txOrder           []string                // Maintain FIFO order
	dedupTxHashSet    map[string]struct{}     // Set for deduplication
	senderTotalAmount map[string]*uint256.Int // Track total amount per sender
	max               int
	netCurrentSlot    atomic.Uint64
	broadcaster       interfaces.Broadcaster
	ledger            interfaces.Ledger

	dedupService *DedupService
	eventRouter  *events.EventRouter                    // Event router for transaction status updates
	txTracker    interfaces.TransactionTrackerInterface // Transaction state tracker
	zkVerify     *zkverify.ZkVerify                     // Zk verify for zk transactions
	txStore      store.TxStore
}

func NewMempool(maxSize int, broadcaster interfaces.Broadcaster, ledger interfaces.Ledger, dedupService *DedupService, eventRouter *events.EventRouter,
	txTracker interfaces.TransactionTrackerInterface, zkVerify *zkverify.ZkVerify, txStore store.TxStore) *Mempool {
	return &Mempool{
		txs:               make(map[string]*transaction.Transaction, maxSize),
		txOrder:           make([]string, 0, maxSize),
		dedupTxHashSet:    make(map[string]struct{}),
		senderTotalAmount: make(map[string]*uint256.Int),
		max:               maxSize,
		netCurrentSlot:    atomic.Uint64{},
		broadcaster:       broadcaster,
		ledger:            ledger,

		dedupService: dedupService,
		eventRouter:  eventRouter,
		txTracker:    txTracker,
		zkVerify:     zkVerify,
		txStore:      txStore,
	}
}

func (mp *Mempool) AddTx(tx *transaction.Transaction, broadcast bool) (string, error) {
	// Generate hash first (read-only operation)
	txHash := tx.Hash()
	txDedupHash := tx.DedupHash()
	monitoring.IncreaseReceivedTxCount()

	// Quick validate transaction
	if err := mp.cheapValidateTransaction(tx); err != nil {
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping invalid tx %s: %v", txHash, err))
		return "", err
	}

	// Now acquire write lock for validation and insertion
	mp.mu.Lock()
	// Validate transaction INSIDE the write lock
	if err := mp.validateTransaction(tx); err != nil {
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping invalid tx %s: %v", txHash, err))
		mp.mu.Unlock()
		return "", err
	}

	logx.Info("MEMPOOL", fmt.Sprintf("Adding tx %+v", tx))

	// Always add to txs and txOrder for compatibility
	mp.txs[txHash] = tx
	mp.txOrder = append(mp.txOrder, txHash)
	mp.dedupTxHashSet[txDedupHash] = struct{}{}

	// Add to sender total amount if transaction is not user content
	if tx.Type != transaction.TxTypeUserContent {
		sender := tx.Sender
		if _, exists := mp.senderTotalAmount[sender]; !exists {
			mp.senderTotalAmount[sender] = new(uint256.Int)
		}
		mp.senderTotalAmount[sender].Add(mp.senderTotalAmount[sender], tx.Amount)
	}
	mp.mu.Unlock()

	monitoring.SetMempoolSize(mp.Size())
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

	if tx.Sender == tx.Recipient {
		return nil
	}

	senderAccount, err := mp.ledger.GetAccount(tx.Sender)
	if err != nil || senderAccount == nil {
		monitoring.RecordRejectedTx(monitoring.TxRejectedUnknown)
		return errors.NewError(errors.ErrCodeAccountNotFound, errors.ErrMsgAccountNotFound)
	}

	// Add nil check for balance
	if senderAccount.Balance == nil {
		monitoring.RecordRejectedTx(monitoring.TxRejectedUnknown)
		return errors.NewError(errors.ErrCodeAccountNotFound, errors.ErrMsgAccountNotFound)
	}

	availableBalance := new(uint256.Int).Set(senderAccount.Balance)

	// Subtract pending amounts in mempool
	totalAmount := mp.senderTotalAmount[tx.Sender]
	if totalAmount != nil {
		availableBalance.Sub(availableBalance, totalAmount)
	}

	if availableBalance.Cmp(tx.Amount) < 0 {
		return errors.NewError(errors.ErrCodeInsufficientFunds, errors.ErrMsgInsufficientFunds)
	}

	return nil
}

func (mp *Mempool) validateNonce(txs []*transaction.Transaction) error {
	minNonce := uint64(1)
	netCurrentSlot := mp.netCurrentSlot.Load()
	if netCurrentSlot > NonceWindow {
		minNonce = netCurrentSlot - NonceWindow
	}
	for _, tx := range txs {
		if tx.Nonce < minNonce {
			return errors.NewError(errors.ErrCodeNonceTooLow, errors.ErrMsgNonceTooLow)
		}
		if tx.Nonce > netCurrentSlot {
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

func (mp *Mempool) validateUserContent(tx *transaction.Transaction) error {
	var content types.UserContent
	if err := json.Unmarshal([]byte(tx.ExtraInfo), &content); err != nil {
		return errors.NewError(errors.ErrCodeInvalidRequest, errors.ErrMsgInvalidUserContent)
	}

	// Check required fields
	if strings.TrimSpace(content.Type) == "" ||
		strings.TrimSpace(content.Title) == "" ||
		strings.TrimSpace(content.Description) == "" {
		return errors.NewError(errors.ErrCodeInvalidRequest, errors.ErrMsgUserContentMissingRequiredFields)
	}

	// Validate address if needed
	if validation.ShouldValidateAddress(content.Type) {
		if !validation.ValidateTxAddress(tx.Recipient) {
			return errors.NewError(errors.ErrCodeInvalidRequest, errors.ErrMsgInvalidTransactionAddress)
		}
	}

	// If content is root => don't need to validate further
	if content.ParentHash == "" && content.RootHash == "" {
		return nil
	}

	// If not root => parentHash and rootHash must be present
	if content.ParentHash == "" || content.RootHash == "" {
		return errors.NewError(errors.ErrCodeInvalidRequest, errors.ErrMsgInvalidUserContent)
	}

	parentTx, err := mp.txStore.GetByHash(content.ParentHash)
	if err != nil {
		return errors.NewError(errors.ErrCodeInvalidRequest, errors.ErrMsgUserContentParentNotFound)
	}

	if parentTx.Type != transaction.TxTypeUserContent ||
		parentTx.Sender != tx.Sender ||
		parentTx.Recipient != tx.Recipient {
		return errors.NewError(errors.ErrCodeInvalidRequest, errors.ErrMsgInvalidUserContent)
	}

	latest, err := mp.txStore.GetLatestVersionContentHash(content.RootHash)
	if err != nil {
		return errors.NewError(errors.ErrCodeInvalidRequest, errors.ErrMsgUserContentRootNotFound)
	}

	if latest != content.ParentHash {
		return errors.NewError(errors.ErrCodeInvalidRequest, errors.ErrMsgUserContentVersionConflict)
	}

	if len(content.ReferenceTxHashes) > 0 {
		referenceTxHashes := content.ReferenceTxHashes
		if len(referenceTxHashes) > validation.MaxReferenceTxs {
			return errors.NewError(errors.ErrCodeInvalidRequest, errors.ErrMsgInvalidUserContent)
		}

		referenceTxs, err := mp.txStore.GetBatch(referenceTxHashes)
		if err != nil {
			return errors.NewError(errors.ErrCodeInternal, errors.ErrMsgInternal)
		}
		if len(referenceTxs) != len(referenceTxHashes) {
			return errors.NewError(errors.ErrCodeInvalidRequest, errors.ErrMsgInvalidUserContent)
		}
	}

	return nil
}

// Cheap validate before acquire write lock
func (mp *Mempool) cheapValidateTransaction(tx *transaction.Transaction) error {
	// Validate amount
	if (tx.Amount == nil || tx.Amount.IsZero()) && (tx.Type != transaction.TxTypeUserContent) {
		monitoring.RecordRejectedTx(monitoring.TxRejectedUnknown)
		return errors.NewError(errors.ErrCodeInvalidAmount, errors.ErrMsgInvalidAmount)
	}

	// Validate memo length (max 64 characters)
	if len(tx.TextData) > MaxMemoCharacters {
		monitoring.RecordRejectedTx(monitoring.TxRejectedUnknown)
		return fmt.Errorf("memo too long: max %d chars, got %d", MaxMemoCharacters, len(tx.TextData))
	}

	// Validate mempool size
	if len(mp.txs) >= mp.max {
		logx.Error("MEMPOOL", "Dropping full mempool")
		monitoring.RecordRejectedTx(monitoring.TxMempoolFull)
		return errors.NewError(errors.ErrCodeMempoolFull, errors.ErrMsgMempoolFull)
	}

	// Validate nonce
	if err := mp.validateNonce([]*transaction.Transaction{tx}); err != nil {
		monitoring.RecordRejectedTx(monitoring.TxInvalidNonce)
		return err
	}

	// Verify signature
	if !tx.Verify(mp.zkVerify) {
		monitoring.RecordRejectedTx(monitoring.TxInvalidSignature)
		return errors.NewError(errors.ErrCodeInvalidSignature, errors.ErrMsgInvalidSignature)
	}

	return nil
}

// Stateless validation, simple for tx
func (mp *Mempool) validateTransaction(tx *transaction.Transaction) error {
	txHash := tx.Hash()
	txDedupHash := tx.DedupHash()

	if _, exists := mp.dedupTxHashSet[txDedupHash]; exists {
		logx.Error("MEMPOOL", fmt.Sprintf("Dropping duplicate tx (double-check) %s", txHash))
		monitoring.RecordRejectedTx(monitoring.TxDuplicated)
		return errors.NewError(errors.ErrCodeDuplicateTransaction, errors.ErrMsgDuplicateTransaction)
	}

	// Check duplicate transaction
	if err := mp.validateDuplicateTxs([]*transaction.Transaction{tx}); err != nil {
		monitoring.RecordRejectedTx(monitoring.TxDuplicated)
		return err
	}

	// Validate user content and skip balance check
	if tx.Type == transaction.TxTypeUserContent {
		if err := mp.validateUserContent(tx); err != nil {
			monitoring.RecordRejectedTx(monitoring.TxInvalidUserContent)
			return err
		}
		return nil
	}

	// Validate balance accounting
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

		sender := tx.Sender
		if totalAmount, exists := mp.senderTotalAmount[sender]; exists {
			totalAmount.Sub(totalAmount, tx.Amount)
			if totalAmount.IsZero() {
				delete(mp.senderTotalAmount, sender)
			}
		}

		// Remove from main buffer + dedup set
		delete(mp.txs, txHash)
		delete(mp.dedupTxHashSet, tx.DedupHash())
	}
	mp.mu.Unlock()

	monitoring.SetMempoolSize(mp.Size())
	mp.dedupService.Add(slot, dedupTxHashes)

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

// GetTransactionCount - read-only operation
func (mp *Mempool) GetTransactionCount() int {
	return mp.Size()
}

// GetTransaction - retrieve transaction data (read-only)
func (mp *Mempool) GetTransaction(txHash string) (*transaction.Transaction, bool) {
	data, exists := mp.txs[txHash]
	return data, exists
}

// GetOrderedTransactions - get transactions in FIFO order (read-only)
func (mp *Mempool) GetOrderedTransactions() []string {
	// Return a copy to avoid external modification
	result := make([]string, len(mp.txOrder))
	copy(result, mp.txOrder)
	return result
}

func (mp *Mempool) SetCurrentSlot(slot uint64) {
	currentSlot := mp.netCurrentSlot.Load()
	if slot <= currentSlot {
		return
	}
	mp.netCurrentSlot.Store(slot)
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
			sender := tx.Sender
			if totalAmount, exists := mp.senderTotalAmount[sender]; exists {
				totalAmount.Sub(totalAmount, tx.Amount)
				if totalAmount.IsZero() {
					delete(mp.senderTotalAmount, sender)
				}
			}

			delete(mp.dedupTxHashSet, tx.DedupHash())
			delete(mp.txs, txHash)
		}
	}

	removedCount := 0
	newTxOrder := make([]string, 0, len(mp.txOrder))
	for _, hash := range mp.txOrder {
		if _, shouldRemove := txHashSet[hash]; shouldRemove {
			removedCount++
			continue
		}
		newTxOrder = append(newTxOrder, hash)
	}
	mp.txOrder = newTxOrder

	// Update monitoring
	if removedCount > 0 {
		monitoring.SetMempoolSize(mp.Size())
	}

	logx.Info("MEMPOOL", fmt.Sprintf("BlockCleanup done — removed %d tx(s) from mempool for slot %d", removedCount, slot))
}
