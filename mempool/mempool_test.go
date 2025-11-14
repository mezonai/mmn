package mempool

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/common"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/types"
)

// ----------------- Helpers / Mocks -----------------

var testPrivateKey ed25519.PrivateKey
var testPublicKey ed25519.PublicKey
var testPublicKeyHex string

var keyPairStore = map[string]struct {
	Private ed25519.PrivateKey
	Public  ed25519.PublicKey
	Hex     string
}{}
var keyPairMu sync.Mutex

func init() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	testPrivateKey = priv
	testPublicKey = pub
	testPublicKeyHex = common.EncodeBytesToBase58(testPublicKey)
}

// getOrCreateKeyPair returns private key and address hex
func getOrCreateKeyPair(name string) (ed25519.PrivateKey, string) {
	keyPairMu.Lock()
	defer keyPairMu.Unlock()
	if v, ok := keyPairStore[name]; ok {
		return v.Private, v.Hex
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	hex := common.EncodeBytesToBase58(pub)
	keyPairStore[name] = struct {
		Private ed25519.PrivateKey
		Public  ed25519.PublicKey
		Hex     string
	}{Private: priv, Public: pub, Hex: hex}
	return keyPairStore[name].Private, keyPairStore[name].Hex
}

func signTransaction(tx *transaction.Transaction, priv ed25519.PrivateKey) {
	// Prefer Serialize() if available
	var b []byte
	if ser := tx.Serialize(); ser != nil {
		b = ser
	} else {
		b = tx.Bytes()
	}
	sig := ed25519.Sign(priv, b)
	tx.Signature = common.EncodeBytesToBase58(sig)
}

func createTestTx(senderName string, nonce uint64, amount uint64) *transaction.Transaction {
	var priv ed25519.PrivateKey
	var senderAddr string
	if senderName == "" {
		priv = testPrivateKey
		senderAddr = testPublicKeyHex
	} else {
		priv, senderAddr = getOrCreateKeyPair(senderName)
	}

	tx := &transaction.Transaction{
		Type:      0,
		Sender:    senderAddr,
		Recipient: "recipient",
		Amount:    uint256.NewInt(amount),
		Timestamp: uint64(time.Now().UnixNano()),
		TextData:  fmt.Sprintf("test-%d", nonce),
		Nonce:     nonce,
	}
	signTransaction(tx, priv)
	return tx
}

// ----------------- Mock Ledger (implements interfaces.Ledger) -----------------

type MockLedger struct {
	balances map[string]*uint256.Int
	nonces   map[string]uint64
	mu       sync.RWMutex
}

func NewMockLedger() *MockLedger {
	l := &MockLedger{
		balances: make(map[string]*uint256.Int),
		nonces:   make(map[string]uint64),
	}
	// seed test account
	l.balances[testPublicKeyHex] = uint256.NewInt(1_000_000)
	l.nonces[testPublicKeyHex] = 0
	return l
}

func (m *MockLedger) SetBalance(addr string, v uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.balances[addr] = uint256.NewInt(v)
}

func (m *MockLedger) SetNonce(addr string, n uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nonces[addr] = n
}

func (m *MockLedger) UpdateNonce(address string, nonce uint64) {
	m.SetNonce(address, nonce)
}

func (m *MockLedger) GetBalance(address string) *uint256.Int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if b, ok := m.balances[address]; ok {
		return b
	}
	return uint256.NewInt(0)
}

func (m *MockLedger) GetNonce(address string) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nonces[address]
}

// Implement interfaces.Ledger methods (minimal but sufficient for tests)

func (m *MockLedger) AccountExists(addr string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.balances[addr]
	return ok, nil
}

func (m *MockLedger) GetAccount(addr string) (*types.Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.balances[addr]
	if !ok {
		return nil, nil
	}
	return &types.Account{
		Address: addr,
		Balance: b,
		Nonce:   m.nonces[addr],
	}, nil
}

func (ml *MockLedger) Balance(addr string) (*uint256.Int, error) {
	if balance, exists := ml.balances[addr]; exists {
		return balance, nil
	}
	return uint256.NewInt(0), nil
}

func (m *MockLedger) CreateAccountsFromGenesis(addresses []config.Address) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range addresses {
		m.balances[a.Address] = a.Amount
		m.nonces[a.Address] = 0
	}
	return nil
}

func (m *MockLedger) GetAccountBatch(addresses []string) (map[string]*types.Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]*types.Account, len(addresses))
	for _, a := range addresses {
		out[a] = &types.Account{
			Address: a,
			Balance: m.balances[a],
			Nonce:   m.nonces[a],
		}
	}
	return out, nil
}

// ----------------- Mock Broadcaster (implements interfaces.Broadcaster) -----------------

type MockBroadcaster struct {
	mu    sync.Mutex
	txs   []*transaction.Transaction
	calls int
}

func (mb *MockBroadcaster) TxBroadcast(ctx context.Context, tx *transaction.Transaction) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.txs = append(mb.txs, tx)
	mb.calls++
	return nil
}

func (mb *MockBroadcaster) BroadcastBlock(ctx context.Context, blk *block.BroadcastedBlock) error {
	// no-op for tests
	return nil
}

func (mb *MockBroadcaster) BroadcastVote(ctx context.Context, vt *consensus.Vote) error {
	// no-op for tests
	return nil
}

func (mb *MockBroadcaster) GetBroadcasted() []*transaction.Transaction {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	out := make([]*transaction.Transaction, len(mb.txs))
	copy(out, mb.txs)
	return out
}

// ----------------- Test DedupService helper (use real DedupService struct) -----------------

// NewTestDedupService returns an empty DedupService suitable for tests (no stores)
func NewTestDedupService() *DedupService {
	return NewDedupService(nil, nil)
}

// ----------------- Tests -----------------

func TestNewMempool_BASIC(t *testing.T) {
	mb := &MockBroadcaster{}
	ledger := NewMockLedger()
	dedup := NewTestDedupService()
	mp := NewMempool(100, mb, ledger, dedup, nil, nil, nil)
	if mp == nil {
		t.Fatal("NewMempool returned nil")
	}
	if mp.Size() != 0 {
		t.Fatalf("expected empty mempool, got %d", mp.Size())
	}
}

func TestAddTx_SuccessAndDuplicate(t *testing.T) {
	mb := &MockBroadcaster{}
	ledger := NewMockLedger()
	dedup := NewTestDedupService()
	mp := NewMempool(10, mb, ledger, dedup, nil, nil, nil)

	// set slot such that minNonce = 1 (netCurrentSlot < NonceWindow)
	mp.SetCurrentSlot(100)

	priv, sender := getOrCreateKeyPair("senderA")
	ledger.SetBalance(sender, 1000)
	ledger.SetNonce(sender, 0)

	tx := createTestTx("senderA", 50, 10) // 50 <= currentSlot 100 => valid
	// ensure signature valid
	signTransaction(tx, priv)

	h, err := mp.AddTx(tx, false)
	if err != nil {
		t.Fatalf("expected add tx success, got err: %v", err)
	}
	if h == "" {
		t.Fatalf("expected non-empty hash")
	}
	if mp.Size() != 1 {
		t.Fatalf("expected size 1 after add, got %d", mp.Size())
	}

	// Add same tx again -> duplicate by dedupTxHashSet
	_, err2 := mp.AddTx(tx, false)
	if err2 == nil {
		t.Fatalf("expected duplicate add to fail")
	}
}

func TestAddTx_FullMempool(t *testing.T) {
	mb := &MockBroadcaster{}
	ledger := NewMockLedger()
	dedup := NewTestDedupService()
	mp := NewMempool(1, mb, ledger, dedup, nil, nil, nil)

	_, s1 := getOrCreateKeyPair("s1")
	_, s2 := getOrCreateKeyPair("s2")
	ledger.SetBalance(s1, 1000)
	ledger.SetBalance(s2, 1000)

	mp.SetCurrentSlot(100)

	tx1 := createTestTx("s1", 10, 1)
	tx2 := createTestTx("s2", 11, 1)

	if _, err := mp.AddTx(tx1, false); err != nil {
		t.Fatalf("add tx1 failed: %v", err)
	}
	if _, err := mp.AddTx(tx2, false); err == nil {
		t.Fatalf("expected add tx2 to fail due mempool full")
	}
}

func TestPullBatch_and_DedupServiceAdd(t *testing.T) {
	mb := &MockBroadcaster{}
	ledger := NewMockLedger()
	dedup := NewTestDedupService()
	mp := NewMempool(10, mb, ledger, dedup, nil, nil, nil)

	mp.SetCurrentSlot(100)

	_, s1 := getOrCreateKeyPair("p1")
	ledger.SetBalance(s1, 1000)

	tx1 := createTestTx("p1", 10, 1)
	tx2 := createTestTx("p1", 11, 1)

	if _, err := mp.AddTx(tx1, false); err != nil {
		t.Fatalf("add tx1 failed: %v", err)
	}
	if _, err := mp.AddTx(tx2, false); err != nil {
		t.Fatalf("add tx2 failed: %v", err)
	}

	// Pull 1
	out := mp.PullBatch(100, 1)
	if len(out) != 1 {
		t.Fatalf("expected 1 tx from PullBatch, got %d", len(out))
	}
	// After pull, mempool size should decrease
	if mp.Size() != 1 {
		t.Fatalf("expected size 1 after pull, got %d", mp.Size())
	}
	// Dedup service should have recorded the pulled dedup hashes (slot 100)
	v, ok := dedup.slotEntry.Load(uint64(100))
	if !ok {
		t.Fatalf("expected dedup service to have slot 100 recorded")
	}
	se := v.(*slotEntry)
	se.mu.Lock()
	count := len(se.txDedupHashes)
	se.mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 hash for slot 100, got %d", count)
	}
}

func TestValidateNonceWindow(t *testing.T) {
	mb := &MockBroadcaster{}
	ledger := NewMockLedger()
	dedup := NewTestDedupService()
	mp := NewMempool(10, mb, ledger, dedup, nil, nil, nil)

	// set current slot 200 -> minNonce = 200 - NonceWindow (150) = 50
	mp.SetCurrentSlot(200)

	_, s := getOrCreateKeyPair("nv")
	ledger.SetBalance(s, 1000)

	// nonce too low (49) -> reject
	txLow := createTestTx("nv", 49, 1)
	if _, err := mp.AddTx(txLow, false); err == nil {
		t.Fatalf("expected nonce-too-low rejected")
	}

	// nonce too high (201) -> reject
	txHigh := createTestTx("nv", 201, 1)
	if _, err := mp.AddTx(txHigh, false); err == nil {
		t.Fatalf("expected nonce-too-high rejected")
	}

	// nonce at boundary (50) -> accept
	txOk := createTestTx("nv", 50, 1)
	if _, err := mp.AddTx(txOk, false); err != nil {
		t.Fatalf("expected nonce at min boundary accepted, got %v", err)
	}
}

func TestBlockCleanupAndVerifyBlockTransactions(t *testing.T) {
	mb := &MockBroadcaster{}
	ledger := NewMockLedger()
	dedup := NewTestDedupService()
	mp := NewMempool(10, mb, ledger, dedup, nil, nil, nil)

	mp.SetCurrentSlot(100)

	_, s1 := getOrCreateKeyPair("c1")
	ledger.SetBalance(s1, 1000)

	tx1 := createTestTx("c1", 10, 1)
	tx2 := createTestTx("c1", 11, 1)

	h1 := tx1.Hash()
	h2 := tx2.Hash()

	if _, err := mp.AddTx(tx1, false); err != nil {
		t.Fatalf("add tx1 failed: %v", err)
	}
	if _, err := mp.AddTx(tx2, false); err != nil {
		t.Fatalf("add tx2 failed: %v", err)
	}

	// VerifyBlockTransactions: ok for these (not duplicate in dedupService)
	if err := mp.VerifyBlockTransactions([]*transaction.Transaction{tx1, tx2}); err != nil {
		t.Fatalf("expected verify ok, got %v", err)
	}

	// simulate dedup service marking one of tx dedup as duplicate
	dh := tx1.DedupHash()
	shardIndex := getShardIndex(dh)
	shard := dedup.txShards[shardIndex]
	shard.mu.Lock()
	shard.txDedupHashSet[dh] = struct{}{}
	shard.mu.Unlock()

	// Store into slotTxDedupHashes (map + lock)
	dedup.Add(1, []string{dh})

	// Now VerifyBlockTransactions should fail (duplicate)
	if err := mp.VerifyBlockTransactions([]*transaction.Transaction{tx1}); err == nil {
		t.Fatalf("expected verify to detect duplicate")
	}

	// Test BlockCleanup removes txs
	txSet := map[string]struct{}{h1: {}, h2: {}}
	mp.BlockCleanup(100, txSet)
	if mp.Size() != 0 {
		t.Fatalf("expected mempool empty after BlockCleanup, got %d", mp.Size())
	}
}

func TestConcurrentAddTxDifferentSenders(t *testing.T) {
	mb := &MockBroadcaster{}
	ledger := NewMockLedger()
	dedup := NewTestDedupService()
	mp := NewMempool(200, mb, ledger, dedup, nil, nil, nil)

	mp.SetCurrentSlot(100)

	// prepare many senders
	for i := 0; i < 20; i++ {
		_, addr := getOrCreateKeyPair(fmt.Sprintf("con%d", i))
		ledger.SetBalance(addr, 10000)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("con%d", id)
			tx := createTestTx(name, uint64(10+id), 1)
			_, _ = mp.AddTx(tx, false)
		}(i)
	}
	wg.Wait()

	if mp.Size() != 20 {
		t.Fatalf("expected 20 tx in mempool after concurrent adds, got %d", mp.Size())
	}
}
