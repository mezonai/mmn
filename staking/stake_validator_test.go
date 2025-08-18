package staking

import (
	"crypto/ed25519"
	"crypto/rand"
	"math/big"
	"testing"
	"time"

	"github.com/mezonai/mmn/types"
	"github.com/mezonai/mmn/poh"
)

func TestStakeValidator_NewStakeValidator(t *testing.T) {
	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	// Create mock PoH service and stake pool
	pohService := &mockPoHService{}
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)

	validator, err := NewStakeValidator(pubKeyStr, privKey, pohService, stakePool)
	if err != nil {
		t.Fatalf("Failed to create stake validator: %v", err)
	}

	if validator.pubkey != pubKeyStr {
		t.Errorf("Pubkey = %v, want %v", validator.pubkey, pubKeyStr)
	}
	if string(validator.privateKey) != string(privKey) {
		t.Errorf("Private key mismatch")
	}
	if validator.pohService != pohService {
		t.Errorf("PoH service mismatch")
	}
	if validator.stakePool != stakePool {
		t.Errorf("Stake pool mismatch")
	}
}

func TestStakeValidator_IsLeader(t *testing.T) {
	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	// Create mock services
	pohService := &mockPoHServiceWithLeader{leaderPubkey: pubKeyStr}
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)

	validator, err := NewStakeValidator(pubKeyStr, privKey, pohService, stakePool)
	if err != nil {
		t.Fatalf("Failed to create stake validator: %v", err)
	}

	// Test when validator is leader
	if !validator.IsLeader(1000) {
		t.Error("Validator should be leader at tick 1000")
	}

	// Create another validator that is not leader
	pohService2 := &mockPoHServiceWithLeader{leaderPubkey: "other_validator"}
	validator2, err := NewStakeValidator(pubKeyStr, privKey, pohService2, stakePool)
	if err != nil {
		t.Fatalf("Failed to create stake validator 2: %v", err)
	}

	if validator2.IsLeader(1000) {
		t.Error("Validator should not be leader at tick 1000")
	}
}

func TestStakeValidator_ValidateBlock(t *testing.T) {
	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	// Create mock services
	pohService := &mockPoHService{}
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)

	validator, err := NewStakeValidator(pubKeyStr, privKey, pohService, stakePool)
	if err != nil {
		t.Fatalf("Failed to create stake validator: %v", err)
	}

	// Create test block
	block := &types.Block{
		Header: types.BlockHeader{
			Height:         1,
			Timestamp:      time.Now().Unix(),
			PrevBlockHash:  "prev_hash",
			MerkleRoot:     "merkle_root",
			ValidatorPubkey: pubKeyStr,
		},
		PoHProof: &types.PoHProof{
			Hash:  "poh_hash",
			Proof: "poh_proof",
			Tick:  1000,
		},
		StakeProof: &types.StakeProof{
			ValidatorPubkey: pubKeyStr,
			Signature:       make([]byte, 64), // Mock signature
			StakeAmount:     big.NewInt(10000000),
		},
		Transactions: []types.Transaction{},
	}

	// Register validator in stake pool first
	tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
	if err != nil {
		t.Fatalf("Failed to create register validator tx: %v", err)
	}
	processor := NewStakeTransactionProcessor(stakePool)
	err = processor.ProcessTransaction(tx, pubKey)
	if err != nil {
		t.Fatalf("Failed to register validator: %v", err)
	}

	// Test block validation
	isValid := validator.ValidateBlock(block)
	if !isValid {
		t.Error("Block should be valid")
	}

	// Test with invalid PoH proof
	block.PoHProof.Hash = "invalid_hash"
	isValid = validator.ValidateBlock(block)
	if isValid {
		t.Error("Block should be invalid with bad PoH proof")
	}
}

func TestStakeValidator_ProduceBlock(t *testing.T) {
	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	// Create mock services
	pohService := &mockPoHServiceWithProof{}
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)

	validator, err := NewStakeValidator(pubKeyStr, privKey, pohService, stakePool)
	if err != nil {
		t.Fatalf("Failed to create stake validator: %v", err)
	}

	// Register validator in stake pool
	tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
	if err != nil {
		t.Fatalf("Failed to create register validator tx: %v", err)
	}
	processor := NewStakeTransactionProcessor(stakePool)
	err = processor.ProcessTransaction(tx, pubKey)
	if err != nil {
		t.Fatalf("Failed to register validator: %v", err)
	}

	// Create test transactions
	transactions := []types.Transaction{
		{
			From:      "sender1",
			To:        "receiver1",
			Amount:    big.NewInt(1000),
			Timestamp: time.Now().Unix(),
		},
	}

	// Produce block
	block, err := validator.ProduceBlock(1, "prev_hash", "merkle_root", transactions)
	if err != nil {
		t.Fatalf("Failed to produce block: %v", err)
	}

	// Verify block structure
	if block.Header.Height != 1 {
		t.Errorf("Block height = %d, want 1", block.Header.Height)
	}
	if block.Header.PrevBlockHash != "prev_hash" {
		t.Errorf("Prev block hash = %v, want 'prev_hash'", block.Header.PrevBlockHash)
	}
	if block.Header.MerkleRoot != "merkle_root" {
		t.Errorf("Merkle root = %v, want 'merkle_root'", block.Header.MerkleRoot)
	}
	if block.Header.ValidatorPubkey != pubKeyStr {
		t.Errorf("Validator pubkey = %v, want %v", block.Header.ValidatorPubkey, pubKeyStr)
	}

	// Verify PoH proof
	if block.PoHProof == nil {
		t.Error("PoH proof should not be nil")
	}
	if block.PoHProof.Hash != "mock_hash" {
		t.Errorf("PoH hash = %v, want 'mock_hash'", block.PoHProof.Hash)
	}
	if block.PoHProof.Proof != "mock_proof" {
		t.Errorf("PoH proof = %v, want 'mock_proof'", block.PoHProof.Proof)
	}

	// Verify stake proof
	if block.StakeProof == nil {
		t.Error("Stake proof should not be nil")
	}
	if block.StakeProof.ValidatorPubkey != pubKeyStr {
		t.Errorf("Stake proof validator = %v, want %v", block.StakeProof.ValidatorPubkey, pubKeyStr)
	}
	if block.StakeProof.StakeAmount.Cmp(big.NewInt(10000000)) != 0 {
		t.Errorf("Stake amount = %v, want %v", block.StakeProof.StakeAmount, big.NewInt(10000000))
	}

	// Verify transactions
	if len(block.Transactions) != 1 {
		t.Errorf("Transaction count = %d, want 1", len(block.Transactions))
	}
	if block.Transactions[0].From != "sender1" {
		t.Errorf("Transaction from = %v, want 'sender1'", block.Transactions[0].From)
	}
}

func TestStakeValidator_SignBlock(t *testing.T) {
	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	// Create mock services
	pohService := &mockPoHService{}
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)

	validator, err := NewStakeValidator(pubKeyStr, privKey, pohService, stakePool)
	if err != nil {
		t.Fatalf("Failed to create stake validator: %v", err)
	}

	// Create test block
	block := &types.Block{
		Header: types.BlockHeader{
			Height:         1,
			Timestamp:      time.Now().Unix(),
			PrevBlockHash:  "prev_hash",
			MerkleRoot:     "merkle_root",
			ValidatorPubkey: pubKeyStr,
		},
	}

	// Sign block
	signature, err := validator.SignBlock(block)
	if err != nil {
		t.Fatalf("Failed to sign block: %v", err)
	}

	if len(signature) != ed25519.SignatureSize {
		t.Errorf("Signature length = %d, want %d", len(signature), ed25519.SignatureSize)
	}

	// Verify signature
	blockHash := validator.calculateBlockHash(block)
	if !ed25519.Verify(pubKey, []byte(blockHash), signature) {
		t.Error("Block signature verification failed")
	}
}

func TestStakeValidator_CreateStakeProof(t *testing.T) {
	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	// Create mock services
	pohService := &mockPoHService{}
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)

	validator, err := NewStakeValidator(pubKeyStr, privKey, pohService, stakePool)
	if err != nil {
		t.Fatalf("Failed to create stake validator: %v", err)
	}

	// Register validator in stake pool
	tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
	if err != nil {
		t.Fatalf("Failed to create register validator tx: %v", err)
	}
	processor := NewStakeTransactionProcessor(stakePool)
	err = processor.ProcessTransaction(tx, pubKey)
	if err != nil {
		t.Fatalf("Failed to register validator: %v", err)
	}

	// Create stake proof
	proof, err := validator.createStakeProof()
	if err != nil {
		t.Fatalf("Failed to create stake proof: %v", err)
	}

	// Verify proof fields
	if proof.ValidatorPubkey != pubKeyStr {
		t.Errorf("Validator pubkey = %v, want %v", proof.ValidatorPubkey, pubKeyStr)
	}
	if proof.StakeAmount.Cmp(big.NewInt(10000000)) != 0 {
		t.Errorf("Stake amount = %v, want %v", proof.StakeAmount, big.NewInt(10000000))
	}
	if len(proof.Signature) != ed25519.SignatureSize {
		t.Errorf("Signature length = %d, want %d", len(proof.Signature), ed25519.SignatureSize)
	}

	// Verify signature
	message := validator.pubkey + proof.StakeAmount.String()
	if !ed25519.Verify(pubKey, []byte(message), proof.Signature) {
		t.Error("Stake proof signature verification failed")
	}
}

func TestStakeValidator_GetStakeInfo(t *testing.T) {
	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	// Create mock services
	pohService := &mockPoHService{}
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)

	validator, err := NewStakeValidator(pubKeyStr, privKey, pohService, stakePool)
	if err != nil {
		t.Fatalf("Failed to create stake validator: %v", err)
	}

	// Test before registration
	info, exists := validator.GetStakeInfo()
	if exists {
		t.Error("Stake info should not exist before registration")
	}

	// Register validator
	tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 8, privKey, 0)
	if err != nil {
		t.Fatalf("Failed to create register validator tx: %v", err)
	}
	processor := NewStakeTransactionProcessor(stakePool)
	err = processor.ProcessTransaction(tx, pubKey)
	if err != nil {
		t.Fatalf("Failed to register validator: %v", err)
	}

	// Test after registration
	info, exists = validator.GetStakeInfo()
	if !exists {
		t.Error("Stake info should exist after registration")
	}
	if info.Pubkey != pubKeyStr {
		t.Errorf("Pubkey = %v, want %v", info.Pubkey, pubKeyStr)
	}
	if info.StakeAmount.Cmp(big.NewInt(10000000)) != 0 {
		t.Errorf("Stake amount = %v, want %v", info.StakeAmount, big.NewInt(10000000))
	}
	if info.Commission != 8 {
		t.Errorf("Commission = %d, want 8", info.Commission)
	}
}

// Extended mock implementations
type mockPoHServiceWithLeader struct {
	leaderPubkey string
}

func (m *mockPoHServiceWithLeader) GetCurrentTick() uint64 { return 1000 }
func (m *mockPoHServiceWithLeader) IsLeader(tick uint64, validatorPubkey string) bool {
	return validatorPubkey == m.leaderPubkey
}
func (m *mockPoHServiceWithLeader) GenerateProof(prevHash string) (string, error) {
	return "proof", nil
}
func (m *mockPoHServiceWithLeader) VerifyProof(hash, proof string) bool { return true }
func (m *mockPoHServiceWithLeader) UpdateLeaderSchedule(schedule []string) error { return nil }

type mockPoHServiceWithProof struct{}

func (m *mockPoHServiceWithProof) GetCurrentTick() uint64 { return 1000 }
func (m *mockPoHServiceWithProof) IsLeader(tick uint64, validatorPubkey string) bool { return true }
func (m *mockPoHServiceWithProof) GenerateProof(prevHash string) (string, error) {
	return "mock_hash", nil
}
func (m *mockPoHServiceWithProof) VerifyProof(hash, proof string) bool {
	return hash == "mock_hash" && proof == "mock_proof"
}
func (m *mockPoHServiceWithProof) UpdateLeaderSchedule(schedule []string) error { return nil }

func BenchmarkStakeValidator_ValidateBlock(b *testing.B) {
	// Setup
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	pohService := &mockPoHService{}
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)

	validator, err := NewStakeValidator(pubKeyStr, privKey, pohService, stakePool)
	if err != nil {
		b.Fatalf("Failed to create stake validator: %v", err)
	}

	// Register validator
	tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
	if err != nil {
		b.Fatalf("Failed to create register validator tx: %v", err)
	}
	processor := NewStakeTransactionProcessor(stakePool)
	err = processor.ProcessTransaction(tx, pubKey)
	if err != nil {
		b.Fatalf("Failed to register validator: %v", err)
	}

	// Create test block
	block := &types.Block{
		Header: types.BlockHeader{
			Height:         1,
			Timestamp:      time.Now().Unix(),
			PrevBlockHash:  "prev_hash",
			MerkleRoot:     "merkle_root",
			ValidatorPubkey: pubKeyStr,
		},
		PoHProof: &types.PoHProof{
			Hash:  "poh_hash",
			Proof: "poh_proof",
			Tick:  1000,
		},
		StakeProof: &types.StakeProof{
			ValidatorPubkey: pubKeyStr,
			Signature:       make([]byte, 64),
			StakeAmount:     big.NewInt(10000000),
		},
		Transactions: []types.Transaction{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateBlock(block)
	}
}

func BenchmarkStakeValidator_ProduceBlock(b *testing.B) {
	// Setup
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	pohService := &mockPoHServiceWithProof{}
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)

	validator, err := NewStakeValidator(pubKeyStr, privKey, pohService, stakePool)
	if err != nil {
		b.Fatalf("Failed to create stake validator: %v", err)
	}

	// Register validator
	tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
	if err != nil {
		b.Fatalf("Failed to create register validator tx: %v", err)
	}
	processor := NewStakeTransactionProcessor(stakePool)
	err = processor.ProcessTransaction(tx, pubKey)
	if err != nil {
		b.Fatalf("Failed to register validator: %v", err)
	}

	transactions := []types.Transaction{
		{
			From:      "sender1",
			To:        "receiver1",
			Amount:    big.NewInt(1000),
			Timestamp: time.Now().Unix(),
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := validator.ProduceBlock(uint64(i), "prev_hash", "merkle_root", transactions)
		if err != nil {
			b.Fatalf("Failed to produce block %d: %v", i, err)
		}
	}
}
