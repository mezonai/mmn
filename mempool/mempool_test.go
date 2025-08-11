package mempool

import (
	"context"
	"encoding/hex"
	"mmn/block"
	"mmn/consensus"
	"mmn/types"
	"sync"
	"testing"
	"time"
)

// MockBroadcaster implements interfaces.Broadcaster for testing
type MockBroadcaster struct {
	broadcastedTxs []*types.Transaction
}

func (mb *MockBroadcaster) BroadcastBlock(ctx context.Context, blk *block.Block) error {
	return nil
}

func (mb *MockBroadcaster) BroadcastVote(ctx context.Context, vt *consensus.Vote) error {
	return nil
}

func (mb *MockBroadcaster) TxBroadcast(ctx context.Context, tx *types.Transaction) error {
	mb.broadcastedTxs = append(mb.broadcastedTxs, tx)
	return nil
}

func (mb *MockBroadcaster) GetBroadcastedTxs() []*types.Transaction {
	return mb.broadcastedTxs
}

func (mb *MockBroadcaster) Reset() {
	mb.broadcastedTxs = nil
}

// Helper function to create a test transaction
func createTestTx(txType int32, sender, recipient string, amount uint64, nonce uint64) *types.Transaction {
	return &types.Transaction{
		Type:      txType,
		Sender:    sender,
		Recipient: recipient,
		Amount:    amount,
		Timestamp: 1234567890,
		TextData:  "test data",
		Nonce:     nonce,
		Signature: "test_signature",
	}
}

func TestNewMempool(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
	maxSize := 100

    mempool := NewMempool(maxSize, mockBroadcaster, nil)

	if mempool == nil {
		t.Fatal("NewMempool returned nil")
	}

	if mempool.max != maxSize {
		t.Errorf("Expected max size %d, got %d", maxSize, mempool.max)
	}

	if mempool.broadcaster != mockBroadcaster {
		t.Error("Broadcaster not set correctly")
	}

	if mempool.txsBuf == nil {
		t.Error("txsBuf map not initialized")
	}

	if len(mempool.txsBuf) != 0 {
		t.Error("txsBuf should be empty initially")
	}
}

func TestMempool_AddTx_Success(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	tx := createTestTx(0, "sender1", "recipient1", 100, 1)

	hash, success := mempool.AddTx(tx, false)

	if !success {
		t.Error("Expected AddTx to succeed")
	}

	if hash == "" {
		t.Error("Expected non-empty hash")
	}

	if mempool.Size() != 1 {
		t.Errorf("Expected size 1, got %d", mempool.Size())
	}
}

func TestMempool_AddTx_WithBroadcast(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	tx := createTestTx(0, "sender1", "recipient1", 100, 1)

	hash, success := mempool.AddTx(tx, true)

	if !success {
		t.Error("Expected AddTx to succeed")
	}

	if hash == "" {
		t.Error("Expected non-empty hash")
	}

	// Wait a bit for the goroutine to complete the broadcast
	time.Sleep(10 * time.Millisecond)

	broadcastedTxs := mockBroadcaster.GetBroadcastedTxs()
	if len(broadcastedTxs) != 1 {
		t.Error("Expected transaction to be broadcasted")
		return
	}

	if broadcastedTxs[0] != tx {
		t.Error("Broadcasted transaction doesn't match original")
	}
}

func TestMempool_AddTx_Duplicate(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	tx := createTestTx(0, "sender1", "recipient1", 100, 1)

	// Add transaction first time
	hash1, success1 := mempool.AddTx(tx, false)
	if !success1 {
		t.Error("Expected first AddTx to succeed")
	}

	// Add same transaction again
	hash2, success2 := mempool.AddTx(tx, false)
	if success2 {
		t.Error("Expected second AddTx to fail (duplicate)")
	}

	if hash2 != "" {
		t.Error("Expected empty hash for duplicate transaction")
	}

	if mempool.Size() != 1 {
		t.Errorf("Expected size 1, got %d", mempool.Size())
	}

	if hash1 == hash2 {
		t.Error("Expected different hashes for duplicate transactions")
	}
}

func TestMempool_AddTx_FullMempool(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(2, mockBroadcaster, nil) // Small size for testing

	// Add first transaction
	tx1 := createTestTx(0, "sender1", "recipient1", 100, 1)
	_, success1 := mempool.AddTx(tx1, false)
	if !success1 {
		t.Error("Expected first AddTx to succeed")
	}

	// Add second transaction
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 2)
	_, success2 := mempool.AddTx(tx2, false)
	if !success2 {
		t.Error("Expected second AddTx to succeed")
	}

	// Try to add third transaction (should fail - mempool full)
	tx3 := createTestTx(0, "sender3", "recipient3", 300, 3)
	hash3, success3 := mempool.AddTx(tx3, false)
	if success3 {
		t.Error("Expected third AddTx to fail (mempool full)")
	}

	if hash3 != "" {
		t.Error("Expected empty hash for rejected transaction")
	}

	if mempool.Size() != 2 {
		t.Errorf("Expected size 2, got %d", mempool.Size())
	}
}

func TestMempool_PullBatch_Empty(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	batch := mempool.PullBatch(5)

	if len(batch) != 0 {
		t.Errorf("Expected empty batch, got %d transactions", len(batch))
	}

	if mempool.Size() != 0 {
		t.Errorf("Expected size 0, got %d", mempool.Size())
	}
}

func TestMempool_PullBatch_Partial(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	// Add 3 transactions
	tx1 := createTestTx(0, "sender1", "recipient1", 100, 1)
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 2)
	tx3 := createTestTx(0, "sender3", "recipient3", 300, 3)

	mempool.AddTx(tx1, false)
	mempool.AddTx(tx2, false)
	mempool.AddTx(tx3, false)

	if mempool.Size() != 3 {
		t.Errorf("Expected size 3, got %d", mempool.Size())
	}

	// Pull batch of size 2
	batch := mempool.PullBatch(2)

	if len(batch) != 2 {
		t.Errorf("Expected batch size 2, got %d", len(batch))
	}

	if mempool.Size() != 1 {
		t.Errorf("Expected remaining size 1, got %d", mempool.Size())
	}

	// Verify batch contains transaction data
	for _, txData := range batch {
		if len(txData) == 0 {
			t.Error("Expected non-empty transaction data in batch")
		}
	}
}

func TestMempool_PullBatch_All(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	// Add 3 transactions
	tx1 := createTestTx(0, "sender1", "recipient1", 100, 1)
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 2)
	tx3 := createTestTx(0, "sender3", "recipient3", 300, 3)

	mempool.AddTx(tx1, false)
	mempool.AddTx(tx2, false)
	mempool.AddTx(tx3, false)

	// Pull batch larger than available transactions
	batch := mempool.PullBatch(5)

	if len(batch) != 3 {
		t.Errorf("Expected batch size 3, got %d", len(batch))
	}

	if mempool.Size() != 0 {
		t.Errorf("Expected size 0, got %d", mempool.Size())
	}
}

func TestMempool_Size(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	// Initial size should be 0
	if mempool.Size() != 0 {
		t.Errorf("Expected initial size 0, got %d", mempool.Size())
	}

	// Add transaction
	tx := createTestTx(0, "sender1", "recipient1", 100, 1)
	mempool.AddTx(tx, false)

	if mempool.Size() != 1 {
		t.Errorf("Expected size 1, got %d", mempool.Size())
	}

	// Add another transaction
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 2)
	mempool.AddTx(tx2, false)

	if mempool.Size() != 2 {
		t.Errorf("Expected size 2, got %d", mempool.Size())
	}

	// Pull batch and check size
	mempool.PullBatch(1)
	if mempool.Size() != 1 {
		t.Errorf("Expected size 1 after pulling batch, got %d", mempool.Size())
	}
}

func TestMempool_ConcurrentAccess(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(100, mockBroadcaster, nil)

	// Test concurrent AddTx operations
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			tx := createTestTx(0, "sender", "recipient", uint64(id), uint64(id))
			mempool.AddTx(tx, false)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Check final size
	if mempool.Size() != 10 {
		t.Errorf("Expected size 10, got %d", mempool.Size())
	}
}

func TestMempool_DifferentTransactionTypes(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	// Test transfer transaction
	txTransfer := createTestTx(0, "sender1", "recipient1", 100, 1)
	hash1, success1 := mempool.AddTx(txTransfer, false)
	if !success1 {
		t.Error("Expected transfer transaction to be added successfully")
	}

	// Test faucet transaction
	txFaucet := createTestTx(1, "sender2", "recipient2", 200, 2)
	hash2, success2 := mempool.AddTx(txFaucet, false)
	if !success2 {
		t.Error("Expected faucet transaction to be added successfully")
	}

	if hash1 == hash2 {
		t.Error("Expected different hashes for different transactions")
	}

	if mempool.Size() != 2 {
		t.Errorf("Expected size 2, got %d", mempool.Size())
	}
}

// New tests for FIFO order and optimized mempool methods

func TestMempool_FIFOOrder(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	// Add transactions in specific order
	tx1 := createTestTx(0, "sender1", "recipient1", 100, 1)
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 2)
	tx3 := createTestTx(0, "sender3", "recipient3", 300, 3)

	hash1, _ := mempool.AddTx(tx1, false)
	hash2, _ := mempool.AddTx(tx2, false)
	hash3, _ := mempool.AddTx(tx3, false)

	// Verify order is maintained
	orderedTxs := mempool.GetOrderedTransactions()
	if len(orderedTxs) != 3 {
		t.Errorf("Expected 3 ordered transactions, got %d", len(orderedTxs))
	}

	if orderedTxs[0] != hash1 {
		t.Error("First transaction should be hash1")
	}
	if orderedTxs[1] != hash2 {
		t.Error("Second transaction should be hash2")
	}
	if orderedTxs[2] != hash3 {
		t.Error("Third transaction should be hash3")
	}
}

func TestMempool_PullBatchFIFOOrder(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	// Add transactions in specific order
	tx1 := createTestTx(0, "sender1", "recipient1", 100, 1)
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 2)
	tx3 := createTestTx(0, "sender3", "recipient3", 300, 3)
	tx4 := createTestTx(0, "sender4", "recipient4", 400, 4)

	mempool.AddTx(tx1, false)
	mempool.AddTx(tx2, false)
	mempool.AddTx(tx3, false)
	mempool.AddTx(tx4, false)

	// Pull batch of size 2 - should get first 2 transactions in order
	batch := mempool.PullBatch(2)
	if len(batch) != 2 {
		t.Errorf("Expected batch size 2, got %d", len(batch))
	}

	// Verify remaining transactions are in correct order
	remainingOrder := mempool.GetOrderedTransactions()
	if len(remainingOrder) != 2 {
		t.Errorf("Expected 2 remaining transactions, got %d", len(remainingOrder))
	}

	// The remaining transactions should be tx3 and tx4 in order
	expectedHash3 := hex.EncodeToString(tx3.Bytes())
	expectedHash4 := hex.EncodeToString(tx4.Bytes())

	if remainingOrder[0] != expectedHash3 {
		t.Error("First remaining transaction should be tx3")
	}
	if remainingOrder[1] != expectedHash4 {
		t.Error("Second remaining transaction should be tx4")
	}
}

func TestMempool_HasTransaction(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	tx := createTestTx(0, "sender1", "recipient1", 100, 1)
	hash, success := mempool.AddTx(tx, false)
	if !success {
		t.Fatal("Failed to add transaction for testing")
	}

	// Test existing transaction
	if !mempool.HasTransaction(hash) {
		t.Error("Expected HasTransaction to return true for existing transaction")
	}

	// Test non-existing transaction
	if mempool.HasTransaction("non_existent_hash") {
		t.Error("Expected HasTransaction to return false for non-existing transaction")
	}
}

func TestMempool_GetTransaction(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	tx := createTestTx(0, "sender1", "recipient1", 100, 1)
	hash, success := mempool.AddTx(tx, false)
	if !success {
		t.Fatal("Failed to add transaction for testing")
	}

	// Test existing transaction
	data, exists := mempool.GetTransaction(hash)
	if !exists {
		t.Error("Expected GetTransaction to return true for existing transaction")
	}
	if len(data) == 0 {
		t.Error("Expected non-empty transaction data")
	}

	// Test non-existing transaction
	data, exists = mempool.GetTransaction("non_existent_hash")
	if exists {
		t.Error("Expected GetTransaction to return false for non-existing transaction")
	}
	if data != nil {
		t.Error("Expected nil data for non-existing transaction")
	}
}

func TestMempool_GetTransactionCount(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	// Initial count should be 0
	if mempool.GetTransactionCount() != 0 {
		t.Errorf("Expected initial count 0, got %d", mempool.GetTransactionCount())
	}

	// Add transactions and check count
	tx1 := createTestTx(0, "sender1", "recipient1", 100, 1)
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 2)

	mempool.AddTx(tx1, false)
	if mempool.GetTransactionCount() != 1 {
		t.Errorf("Expected count 1, got %d", mempool.GetTransactionCount())
	}

	mempool.AddTx(tx2, false)
	if mempool.GetTransactionCount() != 2 {
		t.Errorf("Expected count 2, got %d", mempool.GetTransactionCount())
	}
}

func TestMempool_GetOrderedTransactions(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	// Add transactions
	tx1 := createTestTx(0, "sender1", "recipient1", 100, 1)
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 2)
	tx3 := createTestTx(0, "sender3", "recipient3", 300, 3)

	hash1, _ := mempool.AddTx(tx1, false)
	hash2, _ := mempool.AddTx(tx2, false)
	hash3, _ := mempool.AddTx(tx3, false)

	// Get ordered transactions
	ordered := mempool.GetOrderedTransactions()
	if len(ordered) != 3 {
		t.Errorf("Expected 3 ordered transactions, got %d", len(ordered))
	}

	// Verify order
	if ordered[0] != hash1 {
		t.Error("First transaction should be hash1")
	}
	if ordered[1] != hash2 {
		t.Error("Second transaction should be hash2")
	}
	if ordered[2] != hash3 {
		t.Error("Third transaction should be hash3")
	}

	// Verify that modifying the returned slice doesn't affect the original
	ordered[0] = "modified"
	originalOrder := mempool.GetOrderedTransactions()
	if originalOrder[0] != hash1 {
		t.Error("Modifying returned slice should not affect original")
	}
}

func TestMempool_ConcurrentReadAccess(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(100, mockBroadcaster, nil)

	// Add some transactions first
	for i := 0; i < 5; i++ {
		tx := createTestTx(0, "sender", "recipient", uint64(i), uint64(i))
		mempool.AddTx(tx, false)
	}

	// Test concurrent read operations
	done := make(chan bool, 20)
	for i := 0; i < 10; i++ {
		go func() {
			// Concurrent Size() calls
			size := mempool.Size()
			if size != 5 {
				t.Errorf("Expected size 5, got %d", size)
			}
			done <- true
		}()

		go func() {
			// Concurrent GetTransactionCount() calls
			count := mempool.GetTransactionCount()
			if count != 5 {
				t.Errorf("Expected count 5, got %d", count)
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 20; i++ {
		<-done
	}
}

func TestMempool_ConcurrentReadWriteAccess(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(100, mockBroadcaster, nil)

	// Test concurrent read and write operations
	done := make(chan bool, 20)

	// Writers
	for i := 0; i < 10; i++ {
		go func(id int) {
			tx := createTestTx(0, "sender", "recipient", uint64(id), uint64(id))
			mempool.AddTx(tx, false)
			done <- true
		}(i)
	}

	// Readers
	for i := 0; i < 10; i++ {
		go func() {
			size := mempool.Size()
			if size < 0 || size > 10 {
				t.Errorf("Invalid size: %d", size)
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 20; i++ {
		<-done
	}

	// Final check
	finalSize := mempool.Size()
	if finalSize != 10 {
		t.Errorf("Expected final size 10, got %d", finalSize)
	}
}

func TestMempool_BroadcastWithoutBlocking(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(10, mockBroadcaster, nil)

	// Test that broadcasting doesn't block other operations
	tx := createTestTx(0, "sender1", "recipient1", 100, 1)

	// Add transaction with broadcast
	hash, success := mempool.AddTx(tx, true)
	if !success {
		t.Error("Expected AddTx to succeed")
	}

	// Verify transaction was added
	if !mempool.HasTransaction(hash) {
		t.Error("Expected transaction to be in mempool after broadcast")
	}

	// Wait a bit for the goroutine to complete the broadcast
	time.Sleep(10 * time.Millisecond)

	// Verify broadcast occurred
	broadcastedTxs := mockBroadcaster.GetBroadcastedTxs()
	if len(broadcastedTxs) != 1 {
		t.Error("Expected transaction to be broadcasted")
		return
	}

	// Verify the broadcasted transaction matches the original
	if broadcastedTxs[0] != tx {
		t.Error("Broadcasted transaction doesn't match original")
	}
}

func TestMempool_RaceConditionHandling(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
    mempool := NewMempool(2, mockBroadcaster, nil) // Small size to trigger race conditions

	// Test race condition where multiple goroutines try to add the same transaction
	tx := createTestTx(0, "sender1", "recipient1", 100, 1)

	done := make(chan bool, 5)
	successCount := 0
	var successMutex sync.Mutex

	for i := 0; i < 5; i++ {
		go func() {
			_, success := mempool.AddTx(tx, false)
			if success {
				successMutex.Lock()
				successCount++
				successMutex.Unlock()
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 5; i++ {
		<-done
	}

	// Only one should succeed due to duplicate detection
	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful addition, got %d", successCount)
	}

	// Mempool should have exactly one transaction
	if mempool.Size() != 1 {
		t.Errorf("Expected mempool size 1, got %d", mempool.Size())
	}
}
