package faucet

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/holiman/uint256"
	"github.com/mr-tron/base58"
)

func TestCreateMultisigConfig(t *testing.T) {
	// Generate test keys
	pubKeys := make([]string, 3)
	for i := 0; i < 3; i++ {
		pubKey, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pubKeys[i] = base58.Encode(pubKey)
	}

	// Test valid configuration
	config, err := CreateMultisigConfig(2, pubKeys)
	if err != nil {
		t.Fatalf("Failed to create valid config: %v", err)
	}

	if config.Threshold != 2 {
		t.Errorf("Expected threshold 2, got %d", config.Threshold)
	}

	if len(config.Signers) != 3 {
		t.Errorf("Expected 3 signers, got %d", len(config.Signers))
	}

	if config.Address == "" {
		t.Error("Expected non-empty address")
	}

	// Test invalid threshold
	_, err = CreateMultisigConfig(5, pubKeys)
	if err == nil {
		t.Error("Expected error for invalid threshold")
	}

	// Test insufficient signers
	_, err = CreateMultisigConfig(2, []string{pubKeys[0]})
	if err == nil {
		t.Error("Expected error for insufficient signers")
	}

	// Test duplicate signers
	_, err = CreateMultisigConfig(2, []string{pubKeys[0], pubKeys[0]})
	if err == nil {
		t.Error("Expected error for duplicate signers")
	}
}

func TestMultisigTransaction(t *testing.T) {
	// Generate test keys
	pubKeys := make([]string, 3)
	privKeys := make([][]byte, 3)
	for i := 0; i < 3; i++ {
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pubKeys[i] = base58.Encode(pubKey)
		privKeys[i] = privKey
	}

	// Create multisig config
	config, err := CreateMultisigConfig(2, pubKeys)
	if err != nil {
		t.Fatal(err)
	}

	// Create faucet transaction
	amount, _ := uint256.FromDecimal("100000")
	tx := CreateMultisigFaucetTx(config, "recipient_address", amount, 1, uint64(time.Now().Unix()), "test")

	if tx.Sender != config.Address {
		t.Errorf("Expected sender %s, got %s", config.Address, tx.Sender)
	}

	if tx.Recipient != "recipient_address" {
		t.Errorf("Expected recipient recipient_address, got %s", tx.Recipient)
	}

	if tx.Amount.Cmp(amount) != 0 {
		t.Errorf("Expected amount %s, got %s", amount.String(), tx.Amount.String())
	}

	// Test signing
	sig1, err := SignMultisigTx(tx, pubKeys[0], privKeys[0])
	if err != nil {
		t.Fatalf("Failed to sign with first key: %v", err)
	}

	if sig1.Signer != pubKeys[0] {
		t.Errorf("Expected signer %s, got %s", pubKeys[0], sig1.Signer)
	}

	// Test adding signature
	err = AddSignature(tx, sig1)
	if err != nil {
		t.Fatalf("Failed to add first signature: %v", err)
	}

	if tx.GetSignatureCount() != 1 {
		t.Errorf("Expected 1 signature, got %d", tx.GetSignatureCount())
	}

	// Add second signature
	sig2, err := SignMultisigTx(tx, pubKeys[1], privKeys[1])
	if err != nil {
		t.Fatalf("Failed to sign with second key: %v", err)
	}

	err = AddSignature(tx, sig2)
	if err != nil {
		t.Fatalf("Failed to add second signature: %v", err)
	}

	if tx.GetSignatureCount() != 2 {
		t.Errorf("Expected 2 signatures, got %d", tx.GetSignatureCount())
	}

	if !tx.IsComplete() {
		t.Error("Expected transaction to be complete")
	}

	// Test verification
	err = VerifyMultisigTx(tx)
	if err != nil {
		t.Fatalf("Transaction verification failed: %v", err)
	}
}

func TestMultisigFaucetService(t *testing.T) {
	// Generate test keys
	pubKeys := make([]string, 3)
	privKeys := make([][]byte, 3)
	for i := 0; i < 3; i++ {
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pubKeys[i] = base58.Encode(pubKey)
		privKeys[i] = privKey
	}

	// Create multisig config
	config, err := CreateMultisigConfig(2, pubKeys)
	if err != nil {
		t.Fatal(err)
	}

	// Create service
	maxAmount, _ := uint256.FromDecimal("1000000")
	service := NewMultisigFaucetService(maxAmount, 1*time.Hour)

	// Register config
	err = service.RegisterMultisigConfig(config)
	if err != nil {
		t.Fatalf("Failed to register config: %v", err)
	}

	// Create faucet request
	amount, _ := uint256.FromDecimal("100000")
	tx, err := service.CreateFaucetRequest(config.Address, "recipient", amount, "test")
	if err != nil {
		t.Fatalf("Failed to create faucet request: %v", err)
	}

	txHash := tx.Hash()

	// Add signatures
	err = service.AddSignature(txHash, pubKeys[0], privKeys[0])
	if err != nil {
		t.Fatalf("Failed to add first signature: %v", err)
	}

	err = service.AddSignature(txHash, pubKeys[1], privKeys[1])
	if err != nil {
		t.Fatalf("Failed to add second signature: %v", err)
	}

	// Execute transaction
	executedTx, err := service.VerifyAndExecute(txHash)
	if err != nil {
		t.Fatalf("Failed to execute transaction: %v", err)
	}

	if executedTx == nil {
		t.Error("Expected executed transaction")
	}

	// Test service stats
	stats := service.GetServiceStats()
	if stats.RegisteredConfigs != 1 {
		t.Errorf("Expected 1 registered config, got %d", stats.RegisteredConfigs)
	}

	if stats.PendingTransactions != 0 {
		t.Errorf("Expected 0 pending transactions, got %d", stats.PendingTransactions)
	}
}

func TestMultisigSecurity(t *testing.T) {
	// Generate test keys
	pubKey1, privKey1, _ := ed25519.GenerateKey(rand.Reader)
	pubKey2, privKey2, _ := ed25519.GenerateKey(rand.Reader)
	pubKey3, _, _ := ed25519.GenerateKey(rand.Reader)

	pubKeys := []string{
		base58.Encode(pubKey1),
		base58.Encode(pubKey2),
		base58.Encode(pubKey3),
	}

	// Create multisig config
	config, err := CreateMultisigConfig(2, pubKeys)
	if err != nil {
		t.Fatal(err)
	}

	// Create transaction
	amount, _ := uint256.FromDecimal("100000")
	tx := CreateMultisigFaucetTx(config, "recipient", amount, 1, uint64(time.Now().Unix()), "test")

	// Test unauthorized signer
	unauthorizedPubKey, unauthorizedPrivKey, _ := ed25519.GenerateKey(rand.Reader)
	_, err = SignMultisigTx(tx, base58.Encode(unauthorizedPubKey), unauthorizedPrivKey)
	if err == nil {
		t.Error("Expected error for unauthorized signer")
	}

	// Test valid signatures
	sig1, err := SignMultisigTx(tx, pubKeys[0], privKey1)
	if err != nil {
		t.Fatalf("Failed to create valid signature: %v", err)
	}

	err = AddSignature(tx, sig1)
	if err != nil {
		t.Fatalf("Failed to add valid signature: %v", err)
	}

	sig2, err := SignMultisigTx(tx, pubKeys[1], privKey2)
	if err != nil {
		t.Fatalf("Failed to create second valid signature: %v", err)
	}

	err = AddSignature(tx, sig2)
	if err != nil {
		t.Fatalf("Failed to add second valid signature: %v", err)
	}

	// Verify transaction
	err = VerifyMultisigTx(tx)
	if err != nil {
		t.Fatalf("Valid transaction verification failed: %v", err)
	}
}

func TestMultisigAddressGeneration(t *testing.T) {
	// Generate valid test keys
	pubKeys1 := make([]string, 3)
	pubKeys2 := make([]string, 3)
	pubKeys3 := make([]string, 3)
	
	for i := 0; i < 3; i++ {
		pubKey, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pubKeys1[i] = base58.Encode(pubKey)
		pubKeys2[i] = base58.Encode(pubKey) // Same as pubKeys1
	}

	// Generate different keys for pubKeys3
	for i := 0; i < 3; i++ {
		pubKey, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pubKeys3[i] = base58.Encode(pubKey)
	}

	config1, err := CreateMultisigConfig(2, pubKeys1)
	if err != nil {
		t.Fatal(err)
	}

	config2, err := CreateMultisigConfig(2, pubKeys2)
	if err != nil {
		t.Fatal(err)
	}

	config3, err := CreateMultisigConfig(2, pubKeys3)
	if err != nil {
		t.Fatal(err)
	}

	// Same keys should produce same address
	if config1.Address != config2.Address {
		t.Error("Same signer keys should produce same address")
	}

	// Different keys should produce different address
	if config1.Address == config3.Address {
		t.Error("Different signer keys should produce different address")
	}
}
