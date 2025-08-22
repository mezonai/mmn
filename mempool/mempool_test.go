package mempool

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/consensus"
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
	testPublicKeyHex = hex.EncodeToString(testPublicKey)
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

	pubKeyHex := hex.EncodeToString(pubKey)
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
	broadcastedTxs []*types.Transaction
}

func (mb *MockBroadcaster) BroadcastBlock(ctx context.Context, blk *block.BroadcastedBlock) error {
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

// Helper function to sign a transaction with the test private key
func signTransaction(tx *types.Transaction, privateKey ed25519.PrivateKey) {
	txData := tx.Serialize()
	signature := ed25519.Sign(privateKey, txData)
	tx.Signature = hex.EncodeToString(signature)
}

// Helper function to create a test transaction
func createTestTx(txType int32, sender, recipient string, amount uint64, nonce uint64) *types.Transaction {
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

	tx := &types.Transaction{
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

	mempool := NewMempool(maxSize, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(2, mockBroadcaster, mockLedger) // Small size for testing

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(100, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(100, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(100, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(10, mockBroadcaster, mockLedger)

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
	mempool := NewMempool(2, mockBroadcaster, mockLedger) // Small size to trigger race conditions

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
