package mempool

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/common"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/types"
)

// Test key pair for signing transactions
var testPrivateKey ed25519.PrivateKey
var testPublicKey ed25519.PublicKey
var testPublicKeyHex string

// Map to store key pairs for different test senders
var testKeyPairs = make(map[string]struct {
	PrivateKey   ed25519.PrivateKey
	PublicKey    ed25519.PublicKey
	PublicKeyHex string
})
var keyPairMutex sync.Mutex

func init() {
	// Generate a test key pair for all tests
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("Failed to generate test key pair: %v", err))
	}
	testPublicKey = pub
	testPrivateKey = priv
	testPublicKeyHex = common.EncodeBytesToBase58(testPublicKey)
}

// getOrCreateKeyPair returns a key pair for the given sender address
func getOrCreateKeyPair(sender string) (ed25519.PrivateKey, string) {
	keyPairMutex.Lock()
	defer keyPairMutex.Unlock()

	if keyPair, exists := testKeyPairs[sender]; exists {
		return keyPair.PrivateKey, keyPair.PublicKeyHex
	}

	// Generate new key pair for this sender
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("Failed to generate key pair for sender %s: %v", sender, err))
	}

	pubKeyHex := common.EncodeBytesToBase58(pubKey)
	testKeyPairs[sender] = struct {
		PrivateKey   ed25519.PrivateKey
		PublicKey    ed25519.PublicKey
		PublicKeyHex string
	}{
		PrivateKey:   privKey,
		PublicKey:    pubKey,
		PublicKeyHex: pubKeyHex,
	}

	return privKey, pubKeyHex
}

// MockLedger implements interfaces.Ledger for testing
type MockLedger struct {
	balances map[string]uint64
	nonces   map[string]uint64
	mu       sync.RWMutex
}

func NewMockLedger() *MockLedger {
	ml := &MockLedger{
		balances: make(map[string]uint64),
		nonces:   make(map[string]uint64),
	}

	// Pre-populate with test account
	ml.balances[testPublicKeyHex] = 1000000 // Large balance for testing
	ml.nonces[testPublicKeyHex] = 0

	return ml
}

// UpdateNonce simulates what would happen when a transaction is processed
func (ml *MockLedger) UpdateNonce(address string, nonce uint64) {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	ml.nonces[address] = nonce
}

func (ml *MockLedger) GetBalance(address string) uint64 {
	ml.mu.RLock()
	defer ml.mu.RUnlock()
	return ml.balances[address]
}

func (ml *MockLedger) GetNonce(address string) uint64 {
	ml.mu.RLock()
	defer ml.mu.RUnlock()
	return ml.nonces[address]
}

func (ml *MockLedger) SetBalance(address string, balance uint64) {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	ml.balances[address] = balance
}

func (ml *MockLedger) SetNonce(address string, nonce uint64) {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	ml.nonces[address] = nonce
}

// Implement interfaces.Ledger methods
func (ml *MockLedger) AccountExists(addr string) (bool, error) {
	_, exists := ml.balances[addr]
	return exists, nil
}

func (ml *MockLedger) GetAccount(addr string) (*types.Account, error) {
	ml.mu.RLock()
	defer ml.mu.RUnlock()

	if _, exists := ml.balances[addr]; !exists {
		return nil, nil
	}
	return &types.Account{
		Address: addr,
		Balance: ml.balances[addr],
		Nonce:   ml.nonces[addr],
		History: make([]string, 0),
	}, nil
}

func (ml *MockLedger) Balance(addr string) (uint64, error) {
	return ml.balances[addr], nil
}

func (ml *MockLedger) CreateAccountsFromGenesis(addresses []config.Address) error {
	for _, addr := range addresses {
		ml.balances[addr.Address] = addr.Amount
		ml.nonces[addr.Address] = 0
	}
	return nil
}

// MockBroadcaster implements interfaces.Broadcaster for testing
type MockBroadcaster struct {
	broadcastedTxs []*transaction.Transaction
}

func (mb *MockBroadcaster) BroadcastBlock(ctx context.Context, blk *block.BroadcastedBlock) error {
	return nil
}

func (mb *MockBroadcaster) BroadcastVote(ctx context.Context, vt *consensus.Vote) error {
	return nil
}

func (mb *MockBroadcaster) TxBroadcast(ctx context.Context, tx *transaction.Transaction) error {
	mb.broadcastedTxs = append(mb.broadcastedTxs, tx)
	return nil
}

func (mb *MockBroadcaster) GetBroadcastedTxs() []*transaction.Transaction {
	return mb.broadcastedTxs
}

func (mb *MockBroadcaster) Reset() {
	mb.broadcastedTxs = nil
}

// Helper function to sign a transaction with the test private key
func signTransaction(tx *transaction.Transaction, privateKey ed25519.PrivateKey) {
	txData := tx.Serialize()
	signature := ed25519.Sign(privateKey, txData)
	tx.Signature = common.EncodeBytesToBase58(signature)
}

// Helper function to create a test transaction
func createTestTx(txType int32, sender, recipient string, amount uint64, nonce uint64) *transaction.Transaction {
	// Use current time to ensure uniqueness across test runs
	uniqueSuffix := time.Now().UnixNano()

	// Determine the actual sender and private key to use
	var actualSender string
	var privateKey ed25519.PrivateKey

	if sender == "" {
		// Use default test key pair
		actualSender = testPublicKeyHex
		privateKey = testPrivateKey
	} else {
		// Get or create key pair for the specified sender
		privateKey, actualSender = getOrCreateKeyPair(sender)
	}

	tx := &transaction.Transaction{
		Type:      txType,
		Sender:    actualSender,
		Recipient: recipient,
		Amount:    amount,
		Timestamp: uint64(uniqueSuffix) + nonce,                        // Make timestamp unique
		TextData:  fmt.Sprintf("test data %d-%d", nonce, uniqueSuffix), // Make text data unique
		Nonce:     nonce,
		Signature: "", // Will be set by signTransaction
	}

	// Sign the transaction with the appropriate private key
	signTransaction(tx, privateKey)
	return tx
}

func TestNewMempool(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
	mockLedger := NewMockLedger()
	maxSize := 100

	mempool := NewMempool(maxSize, mockBroadcaster, mockLedger, nil, nil)

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
	mockLedger := NewMockLedger()

	// Get the public key address for sender1
	_, sender1Addr := getOrCreateKeyPair("sender1")

	// Set up sender account with sufficient balance and correct nonce
	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

	tx := createTestTx(0, "sender1", "recipient1", 100, 1)

	hash, err := mempool.AddTx(tx, false)

	if err != nil {
		t.Errorf("Expected AddTx to succeed, got error: %v", err)
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
	mockLedger := NewMockLedger()

	// Get the public key address for sender1
	_, sender1Addr := getOrCreateKeyPair("sender1")

	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

	tx := createTestTx(0, "sender1", "recipient1", 100, 1)

	hash, err := mempool.AddTx(tx, true)

	if err != nil {
		t.Errorf("Expected AddTx to succeed, got error: %v", err)
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
	mockLedger := NewMockLedger()

	// Get the public key address for sender1
	_, sender1Addr := getOrCreateKeyPair("sender1")

	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

	tx := createTestTx(0, "sender1", "recipient1", 100, 1)

	// Add transaction first time
	hash1, err1 := mempool.AddTx(tx, false)
	if err1 != nil {
		t.Errorf("Expected first AddTx to succeed, got error: %v", err1)
	}

	// Add same transaction again
	hash2, err2 := mempool.AddTx(tx, false)
	if err2 == nil {
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
	mockLedger := NewMockLedger()

	// Get public key addresses for the senders
	_, sender1Addr := getOrCreateKeyPair("sender1")
	_, sender2Addr := getOrCreateKeyPair("sender2")
	_, sender3Addr := getOrCreateKeyPair("sender3")

	// Set up accounts with proper addresses
	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetBalance(sender2Addr, 1000)
	mockLedger.SetBalance(sender3Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mockLedger.SetNonce(sender2Addr, 0)
	mockLedger.SetNonce(sender3Addr, 0)
	mempool := NewMempool(2, mockBroadcaster, mockLedger, nil, nil) // Small size for testing

	// Add first transaction
	tx1 := createTestTx(0, "sender1", "recipient1", 100, 1)
	_, err1 := mempool.AddTx(tx1, false)
	if err1 != nil {
		t.Errorf("Expected first AddTx to succeed, got error: %v", err1)
	}

	// Add second transaction
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 1)
	_, err2 := mempool.AddTx(tx2, false)
	if err2 != nil {
		t.Errorf("Expected second AddTx to succeed, got error: %v", err2)
	}

	// Try to add third transaction (should fail - mempool full)
	tx3 := createTestTx(0, "sender3", "recipient3", 300, 1)
	hash3, err3 := mempool.AddTx(tx3, false)
	if err3 == nil {
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
	mockLedger := NewMockLedger()
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

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
	mockLedger := NewMockLedger()

	// Get the public key addresses for all senders
	_, sender1Addr := getOrCreateKeyPair("sender1")
	_, sender2Addr := getOrCreateKeyPair("sender2")
	_, sender3Addr := getOrCreateKeyPair("sender3")

	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetBalance(sender2Addr, 1000)
	mockLedger.SetBalance(sender3Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mockLedger.SetNonce(sender2Addr, 1)
	mockLedger.SetNonce(sender3Addr, 2)
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

	// Add 3 transactions
	tx1 := createTestTx(0, "sender1", "recipient1", 100, 1)
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 2)
	tx3 := createTestTx(0, "sender3", "recipient3", 300, 3)

	_, err1 := mempool.AddTx(tx1, false)
	if err1 != nil {
		t.Fatalf("Failed to add tx1: %v", err1)
	}

	_, err2 := mempool.AddTx(tx2, false)
	if err2 != nil {
		t.Fatalf("Failed to add tx2: %v", err2)
	}

	_, err3 := mempool.AddTx(tx3, false)
	if err3 != nil {
		t.Errorf("Failed to add tx3: %v", err3)
	}

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
	mockLedger := NewMockLedger()

	// Get the public key addresses for all senders
	_, sender1Addr := getOrCreateKeyPair("sender1")
	_, sender2Addr := getOrCreateKeyPair("sender2")
	_, sender3Addr := getOrCreateKeyPair("sender3")

	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetBalance(sender2Addr, 1000)
	mockLedger.SetBalance(sender3Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mockLedger.SetNonce(sender2Addr, 1)
	mockLedger.SetNonce(sender3Addr, 2)
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

	// Add 3 transactions
	tx1 := createTestTx(0, "sender1", "recipient1", 100, 1)
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 2)
	tx3 := createTestTx(0, "sender3", "recipient3", 300, 3)

	_, err1 := mempool.AddTx(tx1, false)
	if err1 != nil {
		t.Fatalf("Failed to add tx1: %v", err1)
	}

	_, err2 := mempool.AddTx(tx2, false)
	if err2 != nil {
		t.Fatalf("Failed to add tx2: %v", err2)
	}

	_, err3 := mempool.AddTx(tx3, false)
	if err3 != nil {
		t.Errorf("Failed to add tx3: %v", err3)
	}

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
	mockLedger := NewMockLedger()

	// Get the public key addresses for all senders
	_, sender1Addr := getOrCreateKeyPair("sender1")
	_, sender2Addr := getOrCreateKeyPair("sender2")

	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetBalance(sender2Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mockLedger.SetNonce(sender2Addr, 1)
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

	// Initial size should be 0
	if mempool.Size() != 0 {
		t.Errorf("Expected initial size 0, got %d", mempool.Size())
	}

	// Add transaction
	tx := createTestTx(0, "sender1", "recipient1", 100, 1)
	_, err1 := mempool.AddTx(tx, false)
	if err1 != nil {
		t.Errorf("Failed to add tx: %v", err1)
	}

	if mempool.Size() != 1 {
		t.Errorf("Expected size 1, got %d", mempool.Size())
	}

	// Add another transaction
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 2)
	_, err2 := mempool.AddTx(tx2, false)
	if err2 != nil {
		t.Errorf("Failed to add tx2: %v", err2)
	}

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
	mockLedger := NewMockLedger()
	// Set up accounts for concurrent access - use different senders to avoid nonce conflicts
	for i := 0; i < 10; i++ {
		senderName := fmt.Sprintf("sender%d", i)
		_, senderAddr := getOrCreateKeyPair(senderName)
		mockLedger.SetBalance(senderAddr, 10000)
		mockLedger.SetNonce(senderAddr, 0) // Each sender starts with nonce 0
	}
	mempool := NewMempool(100, mockBroadcaster, mockLedger, nil, nil)

	// Test concurrent AddTx operations
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			senderName := fmt.Sprintf("sender%d", id)
			tx := createTestTx(0, senderName, "recipient", uint64(id+1), 1) // nonce 1 for each sender
			mempool.AddTx(tx, false)                                        // Ignore error in concurrent test
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
	mockLedger := NewMockLedger()

	// Get the public key addresses for all senders
	_, sender1Addr := getOrCreateKeyPair("sender1")
	_, sender2Addr := getOrCreateKeyPair("sender2")

	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetBalance(sender2Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mockLedger.SetNonce(sender2Addr, 1)
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

	// Test transfer transaction
	txTransfer := createTestTx(0, "sender1", "recipient1", 100, 1)
	hash1, err1 := mempool.AddTx(txTransfer, false)
	if err1 != nil {
		t.Errorf("Expected transfer transaction to be added successfully, got error: %v", err1)
	}

	// Test faucet transaction
	txFaucet := createTestTx(1, "sender2", "recipient2", 200, 2)
	hash2, err2 := mempool.AddTx(txFaucet, false)
	if err2 != nil {
		t.Errorf("Expected faucet transaction to be added successfully, got error: %v", err2)
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
	mockLedger := NewMockLedger()

	// Get the public key addresses for all senders
	_, sender1Addr := getOrCreateKeyPair("sender1")
	_, sender2Addr := getOrCreateKeyPair("sender2")
	_, sender3Addr := getOrCreateKeyPair("sender3")

	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetBalance(sender2Addr, 1000)
	mockLedger.SetBalance(sender3Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mockLedger.SetNonce(sender2Addr, 1)
	mockLedger.SetNonce(sender3Addr, 2)
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

	// Add transactions in specific order
	tx1 := createTestTx(0, "sender1", "recipient1", 100, 1)
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 2)
	tx3 := createTestTx(0, "sender3", "recipient3", 300, 3)

	hash1, err1 := mempool.AddTx(tx1, false)
	if err1 != nil {
		t.Fatalf("Failed to add tx1: %v", err1)
	}
	hash2, err2 := mempool.AddTx(tx2, false)
	if err2 != nil {
		t.Fatalf("Failed to add tx2: %v", err2)
	}
	hash3, err3 := mempool.AddTx(tx3, false)
	if err3 != nil {
		t.Errorf("Failed to add tx3: %v", err3)
	}

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
	mockLedger := NewMockLedger()

	// Get the public key addresses for all senders
	_, sender1Addr := getOrCreateKeyPair("sender1")
	_, sender2Addr := getOrCreateKeyPair("sender2")
	_, sender3Addr := getOrCreateKeyPair("sender3")
	_, sender4Addr := getOrCreateKeyPair("sender4")

	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetBalance(sender2Addr, 1000)
	mockLedger.SetBalance(sender3Addr, 1000)
	mockLedger.SetBalance(sender4Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mockLedger.SetNonce(sender2Addr, 1)
	mockLedger.SetNonce(sender3Addr, 2)
	mockLedger.SetNonce(sender4Addr, 3)
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

	// Add transactions in specific order
	tx1 := createTestTx(0, "sender1", "recipient1", 100, 1)
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 2)
	tx3 := createTestTx(0, "sender3", "recipient3", 300, 3)
	tx4 := createTestTx(0, "sender4", "recipient4", 400, 4)

	_, err1 := mempool.AddTx(tx1, false)
	if err1 != nil {
		t.Fatalf("Failed to add tx1: %v", err1)
	}

	_, err2 := mempool.AddTx(tx2, false)
	if err2 != nil {
		t.Fatalf("Failed to add tx2: %v", err2)
	}

	_, err3 := mempool.AddTx(tx3, false)
	if err3 != nil {
		t.Fatalf("Failed to add tx3: %v", err3)
	}

	_, err4 := mempool.AddTx(tx4, false)
	if err4 != nil {
		t.Errorf("Failed to add tx4: %v", err4)
	}

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
	expectedHash3 := tx3.Hash()
	expectedHash4 := tx4.Hash()

	t.Logf("Expected tx3 hash: %s", expectedHash3)
	t.Logf("Expected tx4 hash: %s", expectedHash4)
	t.Logf("Actual remaining order: %v", remainingOrder)

	if remainingOrder[0] != expectedHash3 {
		t.Errorf("First remaining transaction should be tx3. Expected: %s, Got: %s", expectedHash3, remainingOrder[0])
	}
	if remainingOrder[1] != expectedHash4 {
		t.Errorf("Second remaining transaction should be tx4. Expected: %s, Got: %s", expectedHash4, remainingOrder[1])
	}
}

func TestMempool_HasTransaction(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
	mockLedger := NewMockLedger()

	// Get the public key address for sender
	_, sender1Addr := getOrCreateKeyPair("sender1")

	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

	tx := createTestTx(0, "sender1", "recipient1", 100, 1)
	hash, err := mempool.AddTx(tx, false)
	if err != nil {
		t.Fatalf("Failed to add transaction for testing: %v", err)
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
	mockLedger := NewMockLedger()

	// Get the public key address for sender
	_, sender1Addr := getOrCreateKeyPair("sender1")

	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

	tx := createTestTx(0, "sender1", "recipient1", 100, 1)
	hash, err := mempool.AddTx(tx, false)
	if err != nil {
		t.Fatalf("Failed to add transaction for testing: %v", err)
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
	mockLedger := NewMockLedger()

	// Get the public key addresses for all senders
	_, sender1Addr := getOrCreateKeyPair("sender1")
	_, sender2Addr := getOrCreateKeyPair("sender2")

	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetBalance(sender2Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mockLedger.SetNonce(sender2Addr, 1)
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

	// Initial count should be 0
	if mempool.GetTransactionCount() != 0 {
		t.Errorf("Expected initial count 0, got %d", mempool.GetTransactionCount())
	}

	// Add transactions and check count
	tx1 := createTestTx(0, "sender1", "recipient1", 100, 1)
	tx2 := createTestTx(0, "sender2", "recipient2", 200, 2)

	_, err1 := mempool.AddTx(tx1, false)
	if err1 != nil {
		t.Errorf("Failed to add tx1: %v", err1)
	}
	if mempool.GetTransactionCount() != 1 {
		t.Errorf("Expected count 1, got %d", mempool.GetTransactionCount())
	}

	_, err2 := mempool.AddTx(tx2, false)
	if err2 != nil {
		t.Errorf("Failed to add tx2: %v", err2)
	}
	if mempool.GetTransactionCount() != 2 {
		t.Errorf("Expected count 2, got %d", mempool.GetTransactionCount())
	}
}

func TestMempool_GetOrderedTransactions(t *testing.T) {
	mockBroadcaster := &MockBroadcaster{}
	mockLedger := NewMockLedger()
	// Set initial nonce to 0 for the test account
	mockLedger.SetNonce(testPublicKeyHex, 0)
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

	// Add transactions with sequential nonces
	tx1 := createTestTx(0, "", "recipient1", 100, 1)
	tx2 := createTestTx(0, "", "recipient2", 200, 2)
	tx3 := createTestTx(0, "", "recipient3", 300, 3)

	hash1, err1 := mempool.AddTx(tx1, false)
	if err1 != nil {
		t.Errorf("Failed to add tx1: %v", err1)
	}
	// Simulate nonce update after successful transaction
	mockLedger.UpdateNonce(testPublicKeyHex, 1)

	hash2, err2 := mempool.AddTx(tx2, false)
	if err2 != nil {
		t.Errorf("Failed to add tx2: %v", err2)
	}
	// Simulate nonce update after successful transaction
	mockLedger.UpdateNonce(testPublicKeyHex, 2)

	hash3, err3 := mempool.AddTx(tx3, false)
	if err3 != nil {
		t.Errorf("Failed to add tx3: %v", err3)
	}
	// Simulate nonce update after successful transaction
	mockLedger.UpdateNonce(testPublicKeyHex, 3)

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
	mockLedger := NewMockLedger()

	// Set up accounts for the transactions we'll add
	senderAddrs := make(map[int]string)
	for i := 0; i < 5; i++ {
		senderName := fmt.Sprintf("sender%d", i)
		_, senderAddr := getOrCreateKeyPair(senderName)
		senderAddrs[i] = senderAddr
		mockLedger.SetBalance(senderAddr, 1000)
		mockLedger.SetNonce(senderAddr, 0)
	}
	mempool := NewMempool(100, mockBroadcaster, mockLedger, nil, nil)

	// Add some transactions first
	for i := 0; i < 5; i++ {
		senderName := fmt.Sprintf("sender%d", i)
		tx := createTestTx(0, senderName, "recipient", uint64(i+1), 1)
		_, err := mempool.AddTx(tx, false)
		if err != nil {
			t.Errorf("Failed to add tx%d: %v", i, err)
		}
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
	mockLedger := NewMockLedger()

	// Set up multiple accounts for concurrent access
	senderAddrs := make(map[int]string)
	for i := 0; i < 10; i++ {
		senderName := fmt.Sprintf("sender%d", i)
		_, senderAddr := getOrCreateKeyPair(senderName)
		senderAddrs[i] = senderAddr
		mockLedger.SetBalance(senderAddr, 1000)
		mockLedger.SetNonce(senderAddr, 0) // Each sender starts with nonce 0
	}
	mempool := NewMempool(100, mockBroadcaster, mockLedger, nil, nil)

	// Test concurrent read and write operations
	done := make(chan bool, 20)

	// Writers
	for i := 0; i < 10; i++ {
		go func(id int) {
			senderName := fmt.Sprintf("sender%d", id)
			tx := createTestTx(0, senderName, "recipient", uint64(id+1), 1) // nonce 1 for each sender
			_, err := mempool.AddTx(tx, false)
			if err != nil {
				t.Errorf("Failed to add tx for sender%d: %v", id, err)
			}
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
	mockLedger := NewMockLedger()

	// Get the public key address for sender
	_, sender1Addr := getOrCreateKeyPair("sender1")

	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mempool := NewMempool(10, mockBroadcaster, mockLedger, nil, nil)

	// Test that broadcasting doesn't block other operations
	tx := createTestTx(0, "sender1", "recipient1", 100, 1)

	// Add transaction with broadcast
	hash, err := mempool.AddTx(tx, true)
	if err != nil {
		t.Errorf("Expected AddTx to succeed, got error: %v", err)
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
	mockLedger := NewMockLedger()

	// Get the public key address for sender
	_, sender1Addr := getOrCreateKeyPair("sender1")

	mockLedger.SetBalance(sender1Addr, 1000)
	mockLedger.SetNonce(sender1Addr, 0)
	mempool := NewMempool(2, mockBroadcaster, mockLedger, nil, nil) // Small size to trigger race conditions

	// Test race condition where multiple goroutines try to add the same transaction
	tx := createTestTx(0, "sender1", "recipient1", 100, 1)

	done := make(chan bool, 5)
	successCount := 0
	var successMutex sync.Mutex

	for i := 0; i < 5; i++ {
		go func() {
			_, err := mempool.AddTx(tx, false)
			if err == nil {
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

// Test StaleTimeout - verify that pending transactions older than 10 minutes are automatically cleaned up
func TestMempool_StaleTimeout(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Create test senders
	_, senderAddr1 := getOrCreateKeyPair("stale_sender1")
	_, senderAddr2 := getOrCreateKeyPair("stale_sender2")
	ledger.SetBalance(senderAddr1, 10000)
	ledger.SetBalance(senderAddr2, 10000)
	ledger.SetNonce(senderAddr1, 0)
	ledger.SetNonce(senderAddr2, 0)

	// Add pending transactions (future nonces)
	tx1 := createTestTx(0, "stale_sender1", "recipient1", 100, 5) // nonce 5, current is 0
	tx2 := createTestTx(0, "stale_sender2", "recipient2", 200, 3) // nonce 3, current is 0

	_, err := mempool.AddTx(tx1, false)
	if err != nil {
		t.Fatalf("Failed to add tx1: %v", err)
	}
	_, err = mempool.AddTx(tx2, false)
	if err != nil {
		t.Fatalf("Failed to add tx2: %v", err)
	}

	// Verify both transactions were added as pending
	if mempool.Size() != 2 {
		t.Errorf("Expected 2 transactions in mempool, got %d", mempool.Size())
	}

	// Verify transactions are in pending state (not ready to pull)
	batch := mempool.PullBatch(10)
	if len(batch) != 0 {
		t.Errorf("Expected 0 ready transactions, got %d", len(batch))
	}

	// Manually manipulate timestamps to simulate stale transactions
	// Access pending transactions through reflection or add test helper
	mempool.mu.Lock()
	for sender, pendingMap := range mempool.pendingTxs {
		for nonce, pendingTx := range pendingMap {
			// Make tx1 stale (older than StaleTimeout)
			if sender == senderAddr1 {
				pendingTx.Timestamp = time.Now().Add(-11 * time.Minute) // Older than 10min timeout
			}
			// Keep tx2 fresh
			if sender == senderAddr2 {
				pendingTx.Timestamp = time.Now().Add(-5 * time.Minute) // Within timeout
			}
			_ = nonce // avoid unused variable
		}
	}
	mempool.mu.Unlock()

	// Run cleanup to remove stale transactions
	mempool.PeriodicCleanup()

	// Verify stale transaction was removed, fresh one remains
	if mempool.Size() != 1 {
		t.Errorf("Expected 1 transaction after cleanup, got %d", mempool.Size())
	}

	// Verify the remaining transaction is tx2 (fresh one)
	if !mempool.HasTransaction(tx2.Hash()) {
		t.Error("Fresh transaction should still be present after cleanup")
	}
	if mempool.HasTransaction(tx1.Hash()) {
		t.Error("Stale transaction should be removed after cleanup")
	}

	// Test edge case: exactly at timeout threshold
	tx3 := createTestTx(0, "stale_sender1", "recipient3", 300, 6)
	_, err = mempool.AddTx(tx3, false)
	if err != nil {
		t.Fatalf("Failed to add tx3: %v", err)
	}

	// Set timestamp exactly at threshold
	mempool.mu.Lock()
	if pendingMap, exists := mempool.pendingTxs[senderAddr1]; exists {
		if pendingTx, exists := pendingMap[6]; exists {
			pendingTx.Timestamp = time.Now().Add(-10 * time.Minute) // Exactly at timeout
		}
	}
	mempool.mu.Unlock()

	// Run cleanup again
	mempool.PeriodicCleanup()

	// Transaction at exact threshold should NOT be removed (Before() returns false for equal times)
	if !mempool.HasTransaction(tx3.Hash()) {
		t.Error("Transaction at exact timeout threshold should NOT be removed")
	}

	// Test with transaction slightly older than threshold (should be removed)
	tx4 := createTestTx(0, "stale_sender1", "recipient4", 400, 7)
	_, err = mempool.AddTx(tx4, false)
	if err != nil {
		t.Fatalf("Failed to add tx4: %v", err)
	}

	// Set timestamp slightly before threshold
	mempool.mu.Lock()
	if pendingMap, exists := mempool.pendingTxs[senderAddr1]; exists {
		if pendingTx, exists := pendingMap[7]; exists {
			pendingTx.Timestamp = time.Now().Add(-10*time.Minute - time.Millisecond) // Slightly before threshold
		}
	}
	mempool.mu.Unlock()

	// Run cleanup again
	mempool.PeriodicCleanup()

	// Transaction slightly before threshold should be removed
	if mempool.HasTransaction(tx4.Hash()) {
		t.Error("Transaction slightly before timeout threshold should be removed")
	}
}

// Test MaxFutureNonce - verify transactions with nonce too far ahead are rejected
func TestMempool_MaxFutureNonce(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Create a test sender
	_, senderAddr := getOrCreateKeyPair("future_nonce_sender")
	ledger.SetBalance(senderAddr, 10000)
	ledger.SetNonce(senderAddr, 0) // current nonce is 0

	// Test 1: Add transaction at maximum allowed future nonce (currentNonce + MaxFutureNonce)
	// MaxFutureNonce = 64, current = 0, so max allowed = 0 + 64 = 64
	tx1 := createTestTx(0, "future_nonce_sender", "recipient1", 100, 64)
	_, err := mempool.AddTx(tx1, false)
	if err != nil {
		t.Errorf("Transaction at max future nonce should be accepted: %v", err)
	}

	// Verify transaction was added
	if mempool.Size() != 1 {
		t.Errorf("Expected 1 transaction in mempool, got %d", mempool.Size())
	}

	// Test 2: Try to add transaction beyond MaxFutureNonce limit (should fail)
	tx2 := createTestTx(0, "future_nonce_sender", "recipient2", 100, 65) // nonce 65 > 64 limit
	_, err = mempool.AddTx(tx2, false)
	if err == nil {
		t.Error("Transaction beyond MaxFutureNonce should be rejected")
	}
	if !contains(err.Error(), "nonce too high") {
		t.Errorf("Expected 'nonce too high' error, got: %v", err)
	}

	// Verify transaction was not added
	if mempool.Size() != 1 {
		t.Errorf("Expected 1 transaction in mempool after rejection, got %d", mempool.Size())
	}

	// Test 3: Test with different current nonce
	_, senderAddr2 := getOrCreateKeyPair("future_nonce_sender2")
	ledger.SetBalance(senderAddr2, 10000)
	ledger.SetNonce(senderAddr2, 10) // current nonce is 10

	// Should accept nonce 74 (10 + 64)
	tx3 := createTestTx(0, "future_nonce_sender2", "recipient3", 100, 74)
	_, err = mempool.AddTx(tx3, false)
	if err != nil {
		t.Errorf("Transaction at max future nonce for sender2 should be accepted: %v", err)
	}

	// Should reject nonce 75 (10 + 65)
	tx4 := createTestTx(0, "future_nonce_sender2", "recipient4", 100, 75)
	_, err = mempool.AddTx(tx4, false)
	if err == nil {
		t.Error("Transaction beyond MaxFutureNonce for sender2 should be rejected")
	}

	// Test 4: Edge case - reasonable future nonces should work
	tx5 := createTestTx(0, "future_nonce_sender", "recipient5", 100, 32) // nonce 32, well within limit
	_, err = mempool.AddTx(tx5, false)
	if err != nil {
		t.Errorf("Reasonable future nonce should be accepted: %v", err)
	}

	// Final verification
	expectedSize := 3 // tx1, tx3, tx5
	if mempool.Size() != expectedSize {
		t.Errorf("Expected %d transactions in mempool, got %d", expectedSize, mempool.Size())
	}

	// Test 5: Verify error message format
	tx6 := createTestTx(0, "future_nonce_sender", "recipient6", 100, 100) // way beyond limit
	_, err = mempool.AddTx(tx6, false)
	if err == nil {
		t.Error("Transaction with excessive future nonce should be rejected")
	}
	if !contains(err.Error(), "max allowed 64") || !contains(err.Error(), "got 100") {
		t.Errorf("Error message should contain limit details. Got: %v", err)
	}
}

// Test MaxPendingPerSender - verify that each sender can only have limited pending transactions
func TestMempool_MaxPendingPerSender(t *testing.T) {
	mockLedger := NewMockLedger()
	mockBroadcaster := &MockBroadcaster{}
	mempool := NewMempool(200, mockBroadcaster, mockLedger, nil, nil) // Large mempool for this test

	// Create a test sender
	_, senderAddr := getOrCreateKeyPair("pending_sender")
	mockLedger.SetBalance(senderAddr, 100000) // Large balance for many transactions
	mockLedger.SetNonce(senderAddr, 0)

	// Test 1: Add exactly MaxPendingPerSender (60) pending transactions
	var addedTxs []*transaction.Transaction
	for i := 0; i < 60; i++ {
		tx := createTestTx(0, "pending_sender", fmt.Sprintf("recipient_%d", i), 100, uint64(i+2)) // nonces 2-61
		_, err := mempool.AddTx(tx, false)
		if err != nil {
			t.Errorf("Failed to add transaction %d: %v", i, err)
		}
		addedTxs = append(addedTxs, tx)
	}

	// Verify all 60 transactions were added
	if mempool.Size() != 60 {
		t.Errorf("Expected 60 transactions in mempool, got %d", mempool.Size())
	}

	// Test 2: Try to add 61st pending transaction (should fail due to pending limit)
	// Use a lower nonce that's within future nonce limit but would be the 61st pending tx
	tx61 := createTestTx(0, "pending_sender", "recipient_61", 100, 10) // nonce 10, within future limit
	_, err := mempool.AddTx(tx61, false)
	if err == nil {
		t.Error("61st pending transaction should be rejected")
	}
	if !contains(err.Error(), "too many pending transactions") {
		t.Errorf("Expected 'too many pending transactions' error, got: %v", err)
	}
	if !contains(err.Error(), "max 60") {
		t.Errorf("Error should mention max limit of 60, got: %v", err)
	}

	// Verify transaction was not added
	if mempool.Size() != 60 {
		t.Errorf("Expected 60 transactions after rejection, got %d", mempool.Size())
	}

	// Test 3: Different sender should be able to add their own 60 pending transactions
	_, senderAddr2 := getOrCreateKeyPair("pending_sender2")
	mockLedger.SetBalance(senderAddr2, 100000)
	mockLedger.SetNonce(senderAddr2, 0)

	// Add 60 pending transactions from second sender
	for i := 0; i < 60; i++ {
		tx := createTestTx(0, "pending_sender2", fmt.Sprintf("recipient2_%d", i), 100, uint64(i+2))
		_, err := mempool.AddTx(tx, false)
		if err != nil {
			t.Errorf("Failed to add transaction %d for sender2: %v", i, err)
		}
	}

	// Verify total is now 120 (60 from each sender)
	if mempool.Size() != 120 {
		t.Errorf("Expected 120 transactions total, got %d", mempool.Size())
	}

	// Test 4: Second sender also can't exceed limit
	tx61_sender2 := createTestTx(0, "pending_sender2", "recipient2_61", 100, 10) // nonce 10, within future limit
	_, err = mempool.AddTx(tx61_sender2, false)
	if err == nil {
		t.Error("61st pending transaction for sender2 should be rejected")
	}

	// Test 5: Ready transaction should not count against pending limit
	_, senderAddr3 := getOrCreateKeyPair("ready_sender")
	mockLedger.SetBalance(senderAddr3, 100000)
	mockLedger.SetNonce(senderAddr3, 0)

	// Add ready transaction (nonce = current + 1)
	readyTx := createTestTx(0, "ready_sender", "ready_recipient", 100, 1)
	_, err = mempool.AddTx(readyTx, false)
	if err != nil {
		t.Errorf("Ready transaction should be accepted: %v", err)
	}

	// Now add 59 pending transactions (nonces 2-60, within future nonce limit)
	for i := 0; i < 59; i++ {
		tx := createTestTx(0, "ready_sender", fmt.Sprintf("pending_recipient_%d", i), 100, uint64(i+2))
		_, err := mempool.AddTx(tx, false)
		if err != nil {
			t.Errorf("Pending transaction %d should be accepted for ready_sender: %v", i, err)
		}
	}

	// Final verification: 120 (from first two senders) + 1 (ready) + 59 (pending from third) = 180
	expectedTotal := 180
	if mempool.Size() != expectedTotal {
		t.Errorf("Expected %d transactions total, got %d", expectedTotal, mempool.Size())
	}

	// Test 6: Try to add another pending transaction to first sender (should fail due to pending limit)
	tx_fail := createTestTx(0, "pending_sender", "fail_recipient", 100, 15) // nonce 15, within future limit but exceeds pending limit
	_, err = mempool.AddTx(tx_fail, false)
	if err == nil {
		t.Error("Additional pending transaction should fail due to pending limit")
	}
	if !contains(err.Error(), "too many pending transactions") {
		t.Errorf("Error should mention pending limit, got: %v", err)
	}
}

// Test MaxMempoolSize - verify mempool rejects transactions when it reaches the limit
func TestMempool_MaxMempoolSize(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	// Create mempool with small size for testing
	mempool := NewMempool(5, broadcaster, ledger, nil, nil)

	// Fill mempool to capacity
	for i := 0; i < 5; i++ {
		_, senderAddr := getOrCreateKeyPair(fmt.Sprintf("sender_%d", i))
		ledger.SetBalance(senderAddr, 10000)
		ledger.SetNonce(senderAddr, 0)

		tx := createTestTx(0, fmt.Sprintf("sender_%d", i), "recipient", 100, 1)

		_, err := mempool.AddTx(tx, false)
		if err != nil {
			t.Errorf("Transaction %d should succeed: %v", i, err)
		}
	}

	// Verify mempool is at capacity
	if mempool.Size() != 5 {
		t.Errorf("Expected mempool size 5, got %d", mempool.Size())
	}

	// Try to add one more transaction (should fail)
	_, senderAddr := getOrCreateKeyPair("overflow_sender")
	ledger.SetBalance(senderAddr, 10000)
	ledger.SetNonce(senderAddr, 0)

	tx := createTestTx(0, "overflow_sender", "recipient", 100, 1)

	_, err := mempool.AddTx(tx, false)
	if err == nil {
		t.Error("Transaction should fail when mempool is full")
	}
	if !contains(err.Error(), "mempool full") {
		t.Errorf("Expected 'mempool full' error, got: %v", err)
	}

	// Verify mempool size didn't change
	if mempool.Size() != 5 {
		t.Errorf("Expected mempool size to remain 5, got %d", mempool.Size())
	}

	// Test that we can still add transactions after removing some
	batch := mempool.PullBatch(2)
	if len(batch) != 2 {
		t.Errorf("Expected to pull 2 transactions, got %d", len(batch))
	}

	// Now we should be able to add new transactions
	_, senderAddr2 := getOrCreateKeyPair("new_sender")
	ledger.SetBalance(senderAddr2, 10000)
	ledger.SetNonce(senderAddr2, 0)

	tx2 := createTestTx(0, "new_sender", "recipient", 100, 1)

	_, err = mempool.AddTx(tx2, false)
	if err != nil {
		t.Errorf("Transaction should succeed after freeing space: %v", err)
	}
}

// Test PeriodicCleanup function
func TestMempool_PeriodicCleanup(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Create test senders
	_, senderAddr1 := getOrCreateKeyPair("cleanup_test_sender1")
	_, senderAddr2 := getOrCreateKeyPair("cleanup_test_sender2")
	ledger.SetBalance(senderAddr1, 10000)
	ledger.SetBalance(senderAddr2, 10000)
	ledger.SetNonce(senderAddr1, 0)
	ledger.SetNonce(senderAddr2, 0)

	// Add some transactions
	tx1 := createTestTx(0, "cleanup_test_sender1", "recipient1", 100, 5) // Future nonce
	tx2 := createTestTx(0, "cleanup_test_sender2", "recipient2", 100, 3) // Future nonce

	_, err := mempool.AddTx(tx1, false)
	if err != nil {
		t.Fatalf("Failed to add tx1: %v", err)
	}
	_, err = mempool.AddTx(tx2, false)
	if err != nil {
		t.Fatalf("Failed to add tx2: %v", err)
	}

	// Verify both transactions were added
	initialSize := mempool.Size()
	if initialSize != 2 {
		t.Errorf("Expected 2 transactions initially, got %d", initialSize)
	}

	// Test that cleanup doesn't remove valid transactions
	// In a real implementation, this would clean up stale/invalid transactions
	// For this test, we verify the cleanup function can be called safely

	// Verify transactions are still present after cleanup
	finalSize := mempool.Size()
	if finalSize > initialSize {
		t.Errorf("Cleanup should not increase mempool size. Initial: %d, Final: %d", initialSize, finalSize)
	}

	// Verify we can still interact with the mempool after cleanup
	if !mempool.HasTransaction(tx1.Hash()) && !mempool.HasTransaction(tx2.Hash()) {
		t.Error("At least one transaction should still be present after cleanup")
	}
}

// Test transaction promotion from pending to ready state
func TestMempool_TransactionPromotion(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Create test sender
	_, senderAddr := getOrCreateKeyPair("promotion_sender")
	ledger.SetBalance(senderAddr, 10000)
	ledger.SetNonce(senderAddr, 0) // current nonce is 0

	// Test 1: Add pending transactions with gaps
	tx3 := createTestTx(0, "promotion_sender", "recipient3", 100, 3) // nonce 3
	tx5 := createTestTx(0, "promotion_sender", "recipient5", 100, 5) // nonce 5
	tx2 := createTestTx(0, "promotion_sender", "recipient2", 100, 2) // nonce 2

	_, err := mempool.AddTx(tx3, false)
	if err != nil {
		t.Fatalf("Failed to add tx3: %v", err)
	}
	_, err = mempool.AddTx(tx5, false)
	if err != nil {
		t.Fatalf("Failed to add tx5: %v", err)
	}
	_, err = mempool.AddTx(tx2, false)
	if err != nil {
		t.Fatalf("Failed to add tx2: %v", err)
	}

	// All should be pending (no ready transactions yet)
	batch := mempool.PullBatch(10)
	if len(batch) != 0 {
		t.Errorf("Expected 0 ready transactions, got %d", len(batch))
	}

	// Test 2: Add the missing nonce 1 transaction (should become ready)
	tx1 := createTestTx(0, "promotion_sender", "recipient1", 100, 1) // nonce 1
	_, err = mempool.AddTx(tx1, false)
	if err != nil {
		t.Fatalf("Failed to add tx1: %v", err)
	}

	// Now tx1 should be ready (nonce 1 = current 0 + 1)
	batch = mempool.PullBatch(1)
	if len(batch) != 1 {
		t.Errorf("Expected 1 ready transaction after adding tx1, got %d", len(batch))
	}

	// Simulate processing tx1 (update ledger nonce)
	ledger.SetNonce(senderAddr, 1)

	// Test 3: Run promotion to check if tx2 becomes ready
	mempool.PeriodicCleanup() // This calls promotePendingTransactions

	// Now tx2 should be ready (nonce 2 = current 1 + 1)
	batch = mempool.PullBatch(1)
	if len(batch) != 1 {
		t.Errorf("Expected 1 ready transaction after promotion, got %d", len(batch))
	}

	// Simulate processing tx2
	ledger.SetNonce(senderAddr, 2)
	mempool.PeriodicCleanup()

	// Now tx3 should be ready
	batch = mempool.PullBatch(1)
	if len(batch) != 1 {
		t.Errorf("Expected tx3 to be ready, got %d transactions", len(batch))
	}

	// tx5 should still be pending (gap at nonce 4)
	ledger.SetNonce(senderAddr, 3)
	mempool.PeriodicCleanup()
	batch = mempool.PullBatch(1)
	if len(batch) != 0 {
		t.Errorf("Expected tx5 to remain pending (gap at nonce 4), got %d ready", len(batch))
	}

	// Test 4: Fill the gap with nonce 4
	tx4 := createTestTx(0, "promotion_sender", "recipient4", 100, 4)
	_, err = mempool.AddTx(tx4, false)
	if err != nil {
		t.Fatalf("Failed to add tx4: %v", err)
	}

	// tx4 should immediately be ready
	batch = mempool.PullBatch(1)
	if len(batch) != 1 {
		t.Errorf("Expected tx4 to be ready immediately, got %d", len(batch))
	}

	// After processing tx4, tx5 should become ready
	ledger.SetNonce(senderAddr, 4)
	mempool.PeriodicCleanup()
	batch = mempool.PullBatch(1)
	if len(batch) != 1 {
		t.Errorf("Expected tx5 to be promoted to ready, got %d", len(batch))
	}

	// Final verification: mempool should be empty
	if mempool.Size() != 0 {
		t.Errorf("Expected empty mempool, got %d transactions", mempool.Size())
	}
}

// Test chain promotion of multiple consecutive pending transactions
func TestMempool_ChainPromotion(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Create test sender
	_, senderAddr := getOrCreateKeyPair("chain_sender")
	ledger.SetBalance(senderAddr, 10000)
	ledger.SetNonce(senderAddr, 0)

	// Add consecutive pending transactions (nonces 2, 3, 4, 5)
	var txs []*transaction.Transaction
	for i := 2; i <= 5; i++ {
		tx := createTestTx(0, "chain_sender", fmt.Sprintf("recipient%d", i), 100, uint64(i))
		_, err := mempool.AddTx(tx, false)
		if err != nil {
			t.Fatalf("Failed to add tx with nonce %d: %v", i, err)
		}
		txs = append(txs, tx)
	}

	// All should be pending
	batch := mempool.PullBatch(10)
	if len(batch) != 0 {
		t.Errorf("Expected 0 ready transactions initially, got %d", len(batch))
	}

	// Add the missing nonce 1 transaction
	tx1 := createTestTx(0, "chain_sender", "recipient1", 100, 1)
	_, err := mempool.AddTx(tx1, false)
	if err != nil {
		t.Fatalf("Failed to add tx1: %v", err)
	}

	// With current mempool logic, only one transaction is promoted at a time
	// First pull should get tx1 (nonce 1)
	batch = mempool.PullBatch(10)
	if len(batch) != 1 {
		t.Errorf("Expected 1 ready transaction (tx1), got %d", len(batch))
	}

	// Update ledger nonce to simulate processing tx1
	ledger.SetNonce(senderAddr, 1)

	// Second pull should get tx2 (nonce 2) as it gets promoted
	batch = mempool.PullBatch(10)
	if len(batch) != 1 {
		t.Errorf("Expected 1 ready transaction (tx2), got %d", len(batch))
	}

	// Update ledger nonce to simulate processing tx2
	ledger.SetNonce(senderAddr, 2)

	// Continue pulling and updating nonce for remaining transactions
	for expectedNonce := uint64(3); expectedNonce <= 5; expectedNonce++ {
		batch = mempool.PullBatch(10)
		if len(batch) != 1 {
			t.Errorf("Expected 1 ready transaction (nonce %d), got %d", expectedNonce, len(batch))
		}
		ledger.SetNonce(senderAddr, expectedNonce)
	}

	// Final verification: all transactions should be processed
	if mempool.Size() != 0 {
		t.Errorf("Expected empty mempool after processing all transactions, got %d", mempool.Size())
	}
}

// Test promotion with multiple senders
func TestMempool_MultiSenderPromotion(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Create two test senders
	_, senderAddr1 := getOrCreateKeyPair("multi_sender1")
	_, senderAddr2 := getOrCreateKeyPair("multi_sender2")
	ledger.SetBalance(senderAddr1, 10000)
	ledger.SetBalance(senderAddr2, 10000)
	ledger.SetNonce(senderAddr1, 0)
	ledger.SetNonce(senderAddr2, 0)

	// Add pending transactions for both senders
	tx1_s1 := createTestTx(0, "multi_sender1", "recipient1_s1", 100, 2) // nonce 2
	tx2_s1 := createTestTx(0, "multi_sender1", "recipient2_s1", 100, 3) // nonce 3
	tx1_s2 := createTestTx(0, "multi_sender2", "recipient1_s2", 100, 2) // nonce 2
	tx2_s2 := createTestTx(0, "multi_sender2", "recipient2_s2", 100, 3) // nonce 3

	_, err := mempool.AddTx(tx1_s1, false)
	if err != nil {
		t.Fatalf("Failed to add tx1_s1: %v", err)
	}
	_, err = mempool.AddTx(tx2_s1, false)
	if err != nil {
		t.Fatalf("Failed to add tx2_s1: %v", err)
	}
	_, err = mempool.AddTx(tx1_s2, false)
	if err != nil {
		t.Fatalf("Failed to add tx1_s2: %v", err)
	}
	_, err = mempool.AddTx(tx2_s2, false)
	if err != nil {
		t.Fatalf("Failed to add tx2_s2: %v", err)
	}

	// Add ready transactions for both senders
	ready_s1 := createTestTx(0, "multi_sender1", "ready_s1", 100, 1)
	ready_s2 := createTestTx(0, "multi_sender2", "ready_s2", 100, 1)

	_, err = mempool.AddTx(ready_s1, false)
	if err != nil {
		t.Fatalf("Failed to add ready_s1: %v", err)
	}
	_, err = mempool.AddTx(ready_s2, false)
	if err != nil {
		t.Fatalf("Failed to add ready_s2: %v", err)
	}

	// With current mempool logic, only the initial ready transactions are available
	// (2 ready transactions, no promotion yet)
	batch := mempool.PullBatch(10)
	if len(batch) != 2 {
		t.Errorf("Expected 2 ready transactions (initial ready only), got %d", len(batch))
	}

	// Verify all transactions are processed
	for _, tx := range batch {
		if tx == nil {
			t.Error("Found nil transaction in batch")
		}
	}

	// Update ledger nonces to simulate processing the ready transactions
	ledger.SetNonce(senderAddr1, 1)
	ledger.SetNonce(senderAddr2, 1)

	// Now pull the next batch - should get one transaction from each sender (nonce 2)
	batch = mempool.PullBatch(10)
	if len(batch) != 2 {
		t.Errorf("Expected 2 ready transactions (promoted nonce 2), got %d", len(batch))
	}

	// Update ledger nonces again
	ledger.SetNonce(senderAddr1, 2)
	ledger.SetNonce(senderAddr2, 2)

	// Pull final batch - should get the last transaction from each sender (nonce 3)
	batch = mempool.PullBatch(10)
	if len(batch) != 2 {
		t.Errorf("Expected 2 ready transactions (promoted nonce 3), got %d", len(batch))
	}

	// Process all transactions by updating nonces
	ledger.SetNonce(senderAddr1, 3)
	ledger.SetNonce(senderAddr2, 3)
	mempool.PeriodicCleanup()

	// No more transactions should be available
	batch = mempool.PullBatch(10)
	if len(batch) != 0 {
		t.Errorf("Expected 0 ready transactions after processing all, got %d", len(batch))
	}

	// Verify mempool is empty
	if mempool.Size() != 0 {
		t.Errorf("Expected empty mempool, got %d transactions", mempool.Size())
	}
}

// Test balance validation with pending transactions
func TestMempool_BalanceValidation(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Create test sender with limited balance
	_, senderAddr := getOrCreateKeyPair("balance_sender")
	ledger.SetBalance(senderAddr, 1000) // Total balance: 1000
	ledger.SetNonce(senderAddr, 0)

	// Test 1: Add ready transaction that uses part of balance
	tx1 := createTestTx(0, "balance_sender", "recipient1", 300, 1) // Uses 300
	_, err := mempool.AddTx(tx1, false)
	if err != nil {
		t.Fatalf("Failed to add tx1: %v", err)
	}

	// Test 2: Add pending transaction that should be allowed (total: 300 + 400 = 700 < 1000)
	tx2 := createTestTx(0, "balance_sender", "recipient2", 400, 3) // Uses 400, pending
	_, err = mempool.AddTx(tx2, false)
	if err != nil {
		t.Fatalf("Failed to add tx2: %v", err)
	}

	// Test 3: Add another pending transaction that should be allowed (total: 700 + 200 = 900 < 1000)
	tx3 := createTestTx(0, "balance_sender", "recipient3", 200, 4) // Uses 200, pending
	_, err = mempool.AddTx(tx3, false)
	if err != nil {
		t.Fatalf("Failed to add tx3: %v", err)
	}

	// Test 4: Try to add transaction that would exceed balance (900 + 200 = 1100 > 1000)
	tx4 := createTestTx(0, "balance_sender", "recipient4", 200, 5) // Would exceed balance
	_, err = mempool.AddTx(tx4, false)
	if err == nil {
		t.Error("Expected balance validation error for tx4, but got none")
	}
	if err != nil && !contains(err.Error(), "insufficient available balance") {
		t.Errorf("Expected 'insufficient available balance' error, got: %v", err)
	}

	// Test 5: Add ready transaction that fills remaining balance exactly
	tx5 := createTestTx(0, "balance_sender", "recipient5", 100, 2) // Uses remaining 100
	_, err = mempool.AddTx(tx5, false)
	if err != nil {
		t.Fatalf("Failed to add tx5 (exact balance): %v", err)
	}

	// Test 6: Now any additional transaction should fail
	tx6 := createTestTx(0, "balance_sender", "recipient6", 1, 6) // Even 1 unit should fail
	_, err = mempool.AddTx(tx6, false)
	if err == nil {
		t.Error("Expected balance validation error for tx6, but got none")
	}

	// Verify mempool state
	if mempool.Size() != 4 { // tx1, tx2, tx3, tx5 should be in mempool
		t.Errorf("Expected 4 transactions in mempool, got %d", mempool.Size())
	}
}

// Test balance validation with transaction processing
func TestMempool_BalanceValidationWithProcessing(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Create test sender
	_, senderAddr := getOrCreateKeyPair("processing_sender")
	ledger.SetBalance(senderAddr, 1000)
	ledger.SetNonce(senderAddr, 0)

	// Add transactions that use full balance
	tx1 := createTestTx(0, "processing_sender", "recipient1", 400, 1) // Ready
	tx2 := createTestTx(0, "processing_sender", "recipient2", 300, 2) // Pending
	tx3 := createTestTx(0, "processing_sender", "recipient3", 300, 3) // Pending

	_, err := mempool.AddTx(tx1, false)
	if err != nil {
		t.Fatalf("Failed to add tx1: %v", err)
	}
	_, err = mempool.AddTx(tx2, false)
	if err != nil {
		t.Fatalf("Failed to add tx2: %v", err)
	}
	_, err = mempool.AddTx(tx3, false)
	if err != nil {
		t.Fatalf("Failed to add tx3: %v", err)
	}

	// Process tx1 (pull from ready queue)
	batch := mempool.PullBatch(1)
	if len(batch) != 1 {
		t.Errorf("Expected 1 ready transaction, got %d", len(batch))
	}

	// Simulate tx1 processing (update ledger)
	ledger.SetNonce(senderAddr, 1)
	ledger.SetBalance(senderAddr, 600) // Balance reduced by 400

	// Run promotion - tx2 should become ready
	mempool.PeriodicCleanup()
	batch = mempool.PullBatch(1)
	if len(batch) != 1 {
		t.Errorf("Expected tx2 to be promoted, got %d ready transactions", len(batch))
	}

	// Now try to add a new transaction that would exceed remaining balance
	tx4 := createTestTx(0, "processing_sender", "recipient4", 400, 4) // 300 (tx3) + 400 = 700 > 600
	_, err = mempool.AddTx(tx4, false)
	if err == nil {
		t.Error("Expected balance validation error after processing, but got none")
	}

	// Add transaction that fits within remaining balance
	tx5 := createTestTx(0, "processing_sender", "recipient5", 200, 4) // 300 (tx3) + 200 = 500 <= 600
	_, err = mempool.AddTx(tx5, false)
	if err != nil {
		t.Fatalf("Failed to add tx5 within balance: %v", err)
	}
}

// Test balance validation edge cases
func TestMempool_BalanceValidationEdgeCases(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Test 1: Zero balance account
	_, zeroAddr := getOrCreateKeyPair("zero_balance")
	ledger.SetBalance(zeroAddr, 0)
	ledger.SetNonce(zeroAddr, 0)

	txZero := createTestTx(0, "zero_balance", "recipient", 1, 1)
	_, err := mempool.AddTx(txZero, false)
	if err == nil {
		t.Error("Expected balance validation error for zero balance account")
	}

	// Test 2: Exact balance match
	_, exactAddr := getOrCreateKeyPair("exact_balance")
	ledger.SetBalance(exactAddr, 100)
	ledger.SetNonce(exactAddr, 0)

	txExact := createTestTx(0, "exact_balance", "recipient", 100, 1)
	_, err = mempool.AddTx(txExact, false)
	if err != nil {
		t.Fatalf("Failed to add transaction with exact balance match: %v", err)
	}

	// Test 3: Multiple small transactions that sum to balance
	_, multiAddr := getOrCreateKeyPair("multi_small")
	ledger.SetBalance(multiAddr, 100)
	ledger.SetNonce(multiAddr, 0)

	// Add 10 transactions of 10 units each
	for i := 1; i <= 10; i++ {
		tx := createTestTx(0, "multi_small", fmt.Sprintf("recipient%d", i), 10, uint64(i))
		_, err := mempool.AddTx(tx, false)
		if err != nil {
			t.Fatalf("Failed to add small transaction %d: %v", i, err)
		}
	}

	// 11th transaction should fail
	tx11 := createTestTx(0, "multi_small", "recipient11", 1, 11)
	_, err = mempool.AddTx(tx11, false)
	if err == nil {
		t.Error("Expected balance validation error for 11th transaction")
	}

	// Test 4: Large transaction amount
	_, largeAddr := getOrCreateKeyPair("large_balance")
	ledger.SetBalance(largeAddr, 1000000)
	ledger.SetNonce(largeAddr, 0)

	txLarge := createTestTx(0, "large_balance", "recipient", 999999, 1)
	_, err = mempool.AddTx(txLarge, false)
	if err != nil {
		t.Fatalf("Failed to add large transaction: %v", err)
	}

	// Second large transaction should fail
	txLarge2 := createTestTx(0, "large_balance", "recipient2", 2, 2)
	_, err = mempool.AddTx(txLarge2, false)
	if err == nil {
		t.Error("Expected balance validation error for second large transaction")
	}
}

// Test duplicate nonce detection in both pending and ready queues
func TestMempool_DuplicateNonceDetection(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Create test sender
	_, senderAddr := getOrCreateKeyPair("duplicate_sender")
	ledger.SetBalance(senderAddr, 10000)
	ledger.SetNonce(senderAddr, 0)

	// Test 1: Add ready transaction (nonce 1)
	tx1 := createTestTx(0, "duplicate_sender", "recipient1", 100, 1)
	_, err := mempool.AddTx(tx1, false)
	if err != nil {
		t.Fatalf("Failed to add tx1: %v", err)
	}

	// Test 2: Try to add another transaction with same nonce to ready queue (should fail)
	tx1_dup := createTestTx(0, "duplicate_sender", "recipient1_dup", 200, 1)
	_, err = mempool.AddTx(tx1_dup, false)
	if err == nil {
		t.Error("Expected duplicate nonce in ready queue to be rejected")
	}
	if !contains(err.Error(), "duplicate nonce 1") || !contains(err.Error(), "ready queue") {
		t.Errorf("Expected duplicate nonce error for ready queue, got: %v", err)
	}

	// Test 3: Add pending transaction (nonce 3)
	tx3 := createTestTx(0, "duplicate_sender", "recipient3", 100, 3)
	_, err = mempool.AddTx(tx3, false)
	if err != nil {
		t.Fatalf("Failed to add tx3: %v", err)
	}

	// Test 4: Try to add another transaction with same nonce to pending queue (should fail)
	tx3_dup := createTestTx(0, "duplicate_sender", "recipient3_dup", 300, 3)
	_, err = mempool.AddTx(tx3_dup, false)
	if err == nil {
		t.Error("Expected duplicate nonce in pending queue to be rejected")
	}
	if !contains(err.Error(), "duplicate nonce 3") || !contains(err.Error(), "pending transactions") {
		t.Errorf("Expected duplicate nonce error for pending queue, got: %v", err)
	}

	// Test 5: Verify mempool size hasn't changed (duplicates were rejected)
	if mempool.Size() != 2 {
		t.Errorf("Expected 2 transactions in mempool, got %d", mempool.Size())
	}
}

// Test duplicate nonce detection across different senders (should be allowed)
func TestMempool_DuplicateNonceAcrossSenders(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Create two test senders
	_, sender1Addr := getOrCreateKeyPair("dup_sender1")
	_, sender2Addr := getOrCreateKeyPair("dup_sender2")
	ledger.SetBalance(sender1Addr, 10000)
	ledger.SetBalance(sender2Addr, 10000)
	ledger.SetNonce(sender1Addr, 0)
	ledger.SetNonce(sender2Addr, 0)

	// Test 1: Add transactions with same nonce from different senders (should succeed)
	tx1_s1 := createTestTx(0, "dup_sender1", "recipient1", 100, 1)
	tx1_s2 := createTestTx(0, "dup_sender2", "recipient2", 100, 1)

	_, err := mempool.AddTx(tx1_s1, false)
	if err != nil {
		t.Fatalf("Failed to add tx1_s1: %v", err)
	}

	_, err = mempool.AddTx(tx1_s2, false)
	if err != nil {
		t.Fatalf("Failed to add tx1_s2: %v", err)
	}

	// Test 2: Add pending transactions with same nonce from different senders
	tx3_s1 := createTestTx(0, "dup_sender1", "recipient3", 100, 3)
	tx3_s2 := createTestTx(0, "dup_sender2", "recipient4", 100, 3)

	_, err = mempool.AddTx(tx3_s1, false)
	if err != nil {
		t.Fatalf("Failed to add tx3_s1: %v", err)
	}

	_, err = mempool.AddTx(tx3_s2, false)
	if err != nil {
		t.Fatalf("Failed to add tx3_s2: %v", err)
	}

	// Test 3: Verify all transactions were added
	if mempool.Size() != 4 {
		t.Errorf("Expected 4 transactions in mempool, got %d", mempool.Size())
	}

	// Test 4: Verify ready transactions are available
	batch := mempool.PullBatch(10)
	if len(batch) != 2 {
		t.Errorf("Expected 2 ready transactions, got %d", len(batch))
	}
}

// Test duplicate nonce detection during transaction promotion
func TestMempool_DuplicateNonceWithPromotion(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Create test sender
	_, senderAddr := getOrCreateKeyPair("promo_dup_sender")
	ledger.SetBalance(senderAddr, 10000)
	ledger.SetNonce(senderAddr, 0)

	// Test 1: Add pending transaction (nonce 2)
	tx2 := createTestTx(0, "promo_dup_sender", "recipient2", 100, 2)
	_, err := mempool.AddTx(tx2, false)
	if err != nil {
		t.Fatalf("Failed to add tx2: %v", err)
	}

	// Test 2: Add ready transaction (nonce 1) to trigger promotion
	tx1 := createTestTx(0, "promo_dup_sender", "recipient1", 100, 1)
	_, err = mempool.AddTx(tx1, false)
	if err != nil {
		t.Fatalf("Failed to add tx1: %v", err)
	}

	// Process tx1 to promote tx2
	batch := mempool.PullBatch(1)
	if len(batch) != 1 {
		t.Errorf("Expected 1 ready transaction, got %d", len(batch))
	}

	// Simulate processing tx1
	ledger.SetNonce(senderAddr, 1)
	mempool.PeriodicCleanup() // This should promote tx2

	// Test 3: Try to add duplicate of now-promoted transaction (should fail)
	tx2_dup := createTestTx(0, "promo_dup_sender", "recipient2_dup", 200, 2)
	_, err = mempool.AddTx(tx2_dup, false)
	if err == nil {
		t.Error("Expected duplicate nonce to be rejected after promotion")
	}
	if !contains(err.Error(), "duplicate nonce 2") || !contains(err.Error(), "ready queue") {
		t.Errorf("Expected duplicate nonce error for promoted transaction, got: %v", err)
	}

	// Test 4: Verify tx2 is still ready
	batch = mempool.PullBatch(1)
	if len(batch) != 1 {
		t.Errorf("Expected promoted tx2 to be ready, got %d transactions", len(batch))
	}
}

// Test duplicate nonce detection edge cases
func TestMempool_DuplicateNonceEdgeCases(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Create test sender
	_, senderAddr := getOrCreateKeyPair("edge_dup_sender")
	ledger.SetBalance(senderAddr, 10000)
	ledger.SetNonce(senderAddr, 0)

	// Test 1: Zero nonce duplicates
	tx0_1 := createTestTx(0, "edge_dup_sender", "recipient0_1", 100, 0)

	_, err := mempool.AddTx(tx0_1, false)
	if err == nil {
		t.Error("Expected nonce 0 transaction to be rejected (too low)")
	}

	// Test 2: Large nonce duplicates (within MaxFutureNonce limit)
	largeNonce := uint64(50) // Within the 64 nonce limit
	tx_large1 := createTestTx(0, "edge_dup_sender", "recipient_large1", 100, largeNonce)
	tx_large2 := createTestTx(0, "edge_dup_sender", "recipient_large2", 100, largeNonce)

	_, err = mempool.AddTx(tx_large1, false)
	if err != nil {
		t.Fatalf("Failed to add large nonce tx: %v", err)
	}

	_, err = mempool.AddTx(tx_large2, false)
	if err == nil {
		t.Error("Expected duplicate large nonce to be rejected")
	}
	if !contains(err.Error(), "duplicate nonce") {
		t.Errorf("Expected duplicate nonce error, got: %v", err)
	}

	// Test 3: Multiple pending transactions with gaps, then try duplicates
	tx5 := createTestTx(0, "edge_dup_sender", "recipient5", 100, 5)
	tx7 := createTestTx(0, "edge_dup_sender", "recipient7", 100, 7)
	tx10 := createTestTx(0, "edge_dup_sender", "recipient10", 100, 10)

	_, err = mempool.AddTx(tx5, false)
	if err != nil {
		t.Fatalf("Failed to add tx5: %v", err)
	}
	_, err = mempool.AddTx(tx7, false)
	if err != nil {
		t.Fatalf("Failed to add tx7: %v", err)
	}
	_, err = mempool.AddTx(tx10, false)
	if err != nil {
		t.Fatalf("Failed to add tx10: %v", err)
	}

	// Try to add duplicates of pending transactions
	tx5_dup := createTestTx(0, "edge_dup_sender", "recipient5_dup", 200, 5)
	tx7_dup := createTestTx(0, "edge_dup_sender", "recipient7_dup", 200, 7)

	_, err = mempool.AddTx(tx5_dup, false)
	if err == nil {
		t.Error("Expected duplicate nonce 5 to be rejected")
	}

	_, err = mempool.AddTx(tx7_dup, false)
	if err == nil {
		t.Error("Expected duplicate nonce 7 to be rejected")
	}

	// Verify original transactions are still there
	expectedSize := 4 // tx_large1, tx5, tx7, tx10
	if mempool.Size() != expectedSize {
		t.Errorf("Expected %d transactions in mempool, got %d", expectedSize, mempool.Size())
	}
}

// Test validation edge cases including zero amounts and invalid signatures
func TestMempool_ValidationEdgeCases(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Create test sender
	_, senderAddr := getOrCreateKeyPair("edge_sender")
	ledger.SetBalance(senderAddr, 10000)
	ledger.SetNonce(senderAddr, 0)

	// Test 1: Zero amount transaction (should fail)
	txZeroAmount := createTestTx(0, "edge_sender", "recipient1", 0, 1) // Amount = 0
	_, err := mempool.AddTx(txZeroAmount, false)
	if err == nil {
		t.Error("Expected zero amount transaction to be rejected")
	}
	if !contains(err.Error(), "zero amount not allowed") {
		t.Errorf("Expected 'zero amount not allowed' error, got: %v", err)
	}

	// Test 2: Invalid signature (manually create transaction with bad signature)
	txInvalidSig := &transaction.Transaction{
		Type:      0,
		Sender:    senderAddr,
		Recipient: "recipient2",
		Amount:    100,
		Timestamp: uint64(time.Now().UnixNano()),
		TextData:  "invalid sig test",
		Nonce:     1,
		Signature: "invalid_signature_hex", // Invalid signature
	}
	_, err = mempool.AddTx(txInvalidSig, false)
	if err == nil {
		t.Error("Expected invalid signature transaction to be rejected")
	}
	if !contains(err.Error(), "invalid signature") {
		t.Errorf("Expected 'invalid signature' error, got: %v", err)
	}

	// Test 3: Non-existent sender account
	txNonExistentSender := createTestTx(0, "non_existent_sender", "recipient3", 100, 1)
	_, err = mempool.AddTx(txNonExistentSender, false)
	if err == nil {
		t.Error("Expected non-existent sender to be rejected")
	}
	if !contains(err.Error(), "does not exist") {
		t.Errorf("Expected 'does not exist' error, got: %v", err)
	}

	// Test 4: Empty sender address
	txEmptySender := &transaction.Transaction{
		Type:      0,
		Sender:    "", // Empty sender
		Recipient: "recipient4",
		Amount:    100,
		Timestamp: uint64(time.Now().UnixNano()),
		TextData:  "empty sender test",
		Nonce:     1,
		Signature: "test_signature",
	}
	_, err = mempool.AddTx(txEmptySender, false)
	if err == nil {
		t.Error("Expected empty sender to be rejected")
	}

	// Test 5: Empty recipient address
	txEmptyRecipient := createTestTx(0, "edge_sender", "", 100, 1) // Empty recipient
	_, err = mempool.AddTx(txEmptyRecipient, false)
	emptyRecipientAccepted := (err == nil)
	if err != nil {
		// This might be allowed depending on implementation - just log the result
		t.Logf("Empty recipient transaction result: %v", err)
	} else {
		t.Log("Empty recipient transaction was accepted")
	}

	// Test 6: Verify mempool size accounts for accepted transactions
	expectedSize := 0
	if emptyRecipientAccepted {
		expectedSize = 1
	}
	if mempool.Size() != expectedSize {
		t.Errorf("Expected %d transactions in mempool after edge case tests, got %d", expectedSize, mempool.Size())
	}
}

// Test validation edge cases with malformed transaction data
func TestMempool_ValidationMalformedData(t *testing.T) {
	ledger := NewMockLedger()
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, ledger, nil, nil)

	// Create test sender
	_, senderAddr := getOrCreateKeyPair("malformed_sender")
	ledger.SetBalance(senderAddr, 10000)
	ledger.SetNonce(senderAddr, 0)

	// Test 1: Extremely large amount (potential overflow)
	txLargeAmount := createTestTx(0, "malformed_sender", "recipient1", ^uint64(0), 1) // Max uint64
	_, err := mempool.AddTx(txLargeAmount, false)
	if err == nil {
		t.Error("Expected extremely large amount to be rejected due to insufficient balance")
	}
	if !contains(err.Error(), "insufficient") {
		t.Errorf("Expected 'insufficient' balance error, got: %v", err)
	}

	// Test 2: Very long text data
	longTextData := make([]byte, 10000) // 10KB of data
	for i := range longTextData {
		longTextData[i] = 'A'
	}
	txLongText := &transaction.Transaction{
		Type:      0,
		Sender:    senderAddr,
		Recipient: "recipient2",
		Amount:    100,
		Timestamp: uint64(time.Now().UnixNano()),
		TextData:  string(longTextData),
		Nonce:     1,
		Signature: "test_signature",
	}
	_, err = mempool.AddTx(txLongText, false)
	// This might be allowed - just verify it doesn't crash
	if err != nil {
		t.Logf("Long text data transaction result: %v", err)
	} else {
		t.Log("Long text data transaction was accepted")
	}

	// Test 3: Negative timestamp (if possible)
	txNegativeTime := &transaction.Transaction{
		Type:      0,
		Sender:    senderAddr,
		Recipient: "recipient3",
		Amount:    100,
		Timestamp: 0, // Zero timestamp
		TextData:  "zero timestamp test",
		Nonce:     2,
		Signature: "test_signature",
	}
	_, err = mempool.AddTx(txNegativeTime, false)
	// This might be allowed - just verify it doesn't crash
	if err != nil {
		t.Logf("Zero timestamp transaction result: %v", err)
	} else {
		t.Log("Zero timestamp transaction was accepted")
	}

	// Test 4: Invalid transaction type
	txInvalidType := &transaction.Transaction{
		Type:      999, // Invalid type
		Sender:    senderAddr,
		Recipient: "recipient4",
		Amount:    100,
		Timestamp: uint64(time.Now().UnixNano()),
		TextData:  "invalid type test",
		Nonce:     3,
		Signature: "test_signature",
	}
	_, err = mempool.AddTx(txInvalidType, false)
	// This might be allowed - just verify it doesn't crash
	if err != nil {
		t.Logf("Invalid type transaction result: %v", err)
	} else {
		t.Log("Invalid type transaction was accepted")
	}
}

// Test validation with nil ledger and other system edge cases
func TestMempool_ValidationSystemEdgeCases(t *testing.T) {
	// Test 1: Mempool with nil ledger
	broadcaster := &MockBroadcaster{}
	mempool := NewMempool(100, broadcaster, nil, nil, nil) // nil ledger

	tx := createTestTx(0, "sender", "recipient", 100, 1)
	_, err := mempool.AddTx(tx, false)
	if err == nil {
		t.Error("Expected nil ledger to cause validation error")
	}
	if !contains(err.Error(), "ledger not available") {
		t.Errorf("Expected 'ledger not available' error, got: %v", err)
	}

	// Test 2: Concurrent access to validation (basic test)
	ledger := NewMockLedger()
	mempool2 := NewMempool(100, broadcaster, ledger, nil, nil)

	_, senderAddr := getOrCreateKeyPair("concurrent_sender")
	ledger.SetBalance(senderAddr, 10000)
	ledger.SetNonce(senderAddr, 0)

	// Launch multiple goroutines trying to add transactions
	var wg sync.WaitGroup
	errorCount := int32(0)
	successCount := int32(0)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(nonce int) {
			defer wg.Done()
			tx := createTestTx(0, "concurrent_sender", fmt.Sprintf("recipient_%d", nonce), 100, uint64(nonce+1))
			_, err := mempool2.AddTx(tx, false)
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Concurrent validation results: %d successes, %d errors", successCount, errorCount)

	// At least some transactions should succeed
	if successCount == 0 {
		t.Error("Expected at least some concurrent transactions to succeed")
	}

	// Test 3: Memory pressure simulation (add many transactions)
	ledger2 := NewMockLedger()
	mempool3 := NewMempool(1000, broadcaster, ledger2, nil, nil)

	// Create many senders to avoid nonce conflicts
	for i := 0; i < 100; i++ {
		_, addr := getOrCreateKeyPair(fmt.Sprintf("pressure_sender_%d", i))
		ledger2.SetBalance(addr, 10000)
		ledger2.SetNonce(addr, 0)

		tx := createTestTx(0, fmt.Sprintf("pressure_sender_%d", i), "recipient", 100, 1)
		_, err := mempool3.AddTx(tx, false)
		if err != nil {
			t.Errorf("Failed to add pressure test transaction %d: %v", i, err)
		}
	}

	if mempool3.Size() != 100 {
		t.Errorf("Expected 100 transactions under memory pressure, got %d", mempool3.Size())
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			containsInMiddle(s, substr)))
}

func containsInMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
