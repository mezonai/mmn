package staking

import (
	"crypto/ed25519"
	"crypto/rand"
	"math/big"
	"testing"
	"time"
)

func TestStakeTransactionProcessor_ProcessTransaction(t *testing.T) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10, 8640)
	processor := NewStakeTransactionProcessor(stakePool)

	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	tests := []struct {
		name        string
		setupTx     func() *StakeTransaction
		wantErr     bool
		errContains string
	}{
		{
			name: "Valid register validator transaction",
			setupTx: func() *StakeTransaction {
				tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
				if err != nil {
					t.Fatalf("Failed to create register validator tx: %v", err)
				}
				return tx
			},
			wantErr: false,
		},
		{
			name: "Register validator with insufficient stake",
			setupTx: func() *StakeTransaction {
				tx, err := CreateRegisterValidatorTx(pubKeyStr+"2", big.NewInt(500000), 5, privKey, 0)
				if err != nil {
					t.Fatalf("Failed to create register validator tx: %v", err)
				}
				return tx
			},
			wantErr:     true,
			errContains: "stake amount 500000 is below minimum 1000000",
		},
		{
			name: "Delegate to existing validator",
			setupTx: func() *StakeTransaction {
				// First register a validator
				regTx, err := CreateRegisterValidatorTx(pubKeyStr+"_validator", big.NewInt(10000000), 5, privKey, 0)
				if err != nil {
					t.Fatalf("Failed to create register validator tx: %v", err)
				}
				err = processor.ProcessTransaction(regTx, pubKey)
				if err != nil {
					t.Fatalf("Failed to process register validator tx: %v", err)
				}
				processor.nonces[pubKeyStr+"_validator"]++ // Update nonce manually for test

				// Create delegation transaction
				tx, err := CreateDelegateTx(pubKeyStr+"_delegator", pubKeyStr+"_validator", big.NewInt(5000000), privKey, 0)
				if err != nil {
					t.Fatalf("Failed to create delegate tx: %v", err)
				}
				return tx
			},
			wantErr: false,
		},
		{
			name: "Invalid nonce",
			setupTx: func() *StakeTransaction {
				tx, err := CreateRegisterValidatorTx(pubKeyStr+"_invalid_nonce", big.NewInt(10000000), 5, privKey, 999) // Wrong nonce
				if err != nil {
					t.Fatalf("Failed to create register validator tx: %v", err)
				}
				return tx
			},
			wantErr:     true,
			errContains: "invalid nonce",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := tt.setupTx()
			err := processor.ProcessTransaction(tx, pubKey)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ProcessTransaction() expected error but got none")
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("ProcessTransaction() error = %v, want to contain %v", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("ProcessTransaction() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestCreateRegisterValidatorTx(t *testing.T) {
	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
	if err != nil {
		t.Fatalf("Failed to create register validator tx: %v", err)
	}

	// Verify transaction fields
	if tx.Type != StakeTxTypeRegisterValidator {
		t.Errorf("Transaction type = %v, want %v", tx.Type, StakeTxTypeRegisterValidator)
	}
	if tx.ValidatorPubkey != pubKeyStr {
		t.Errorf("Validator pubkey = %v, want %v", tx.ValidatorPubkey, pubKeyStr)
	}
	if tx.DelegatorPubkey != pubKeyStr {
		t.Errorf("Delegator pubkey = %v, want %v", tx.DelegatorPubkey, pubKeyStr)
	}
	if tx.Amount.Cmp(big.NewInt(10000000)) != 0 {
		t.Errorf("Amount = %v, want %v", tx.Amount, big.NewInt(10000000))
	}
	if tx.Commission != 5 {
		t.Errorf("Commission = %v, want %v", tx.Commission, 5)
	}
	if tx.Nonce != 0 {
		t.Errorf("Nonce = %v, want %v", tx.Nonce, 0)
	}
	if len(tx.Signature) == 0 {
		t.Errorf("Signature is empty")
	}

	// Verify signature is valid
	processor := NewStakeTransactionProcessor(nil)
	if !processor.verifyStakeTransaction(tx, pubKey) {
		t.Errorf("Transaction signature verification failed")
	}
}

func TestCreateDelegateTx(t *testing.T) {
	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	delegatorPubkey := string(pubKey)
	validatorPubkey := "validator_pubkey"
	amount := big.NewInt(5000000)
	nonce := uint64(1)

	tx, err := CreateDelegateTx(delegatorPubkey, validatorPubkey, amount, privKey, nonce)
	if err != nil {
		t.Fatalf("Failed to create delegate tx: %v", err)
	}

	// Verify transaction fields
	if tx.Type != StakeTxTypeDelegate {
		t.Errorf("Transaction type = %v, want %v", tx.Type, StakeTxTypeDelegate)
	}
	if tx.DelegatorPubkey != delegatorPubkey {
		t.Errorf("Delegator pubkey = %v, want %v", tx.DelegatorPubkey, delegatorPubkey)
	}
	if tx.ValidatorPubkey != validatorPubkey {
		t.Errorf("Validator pubkey = %v, want %v", tx.ValidatorPubkey, validatorPubkey)
	}
	if tx.Amount.Cmp(amount) != 0 {
		t.Errorf("Amount = %v, want %v", tx.Amount, amount)
	}
	if tx.Nonce != nonce {
		t.Errorf("Nonce = %v, want %v", tx.Nonce, nonce)
	}

	// Verify signature
	processor := NewStakeTransactionProcessor(nil)
	if !processor.verifyStakeTransaction(tx, pubKey) {
		t.Errorf("Transaction signature verification failed")
	}
}

func TestCreateUndelegateTx(t *testing.T) {
	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	delegatorPubkey := string(pubKey)
	validatorPubkey := "validator_pubkey"
	amount := big.NewInt(2000000)
	nonce := uint64(2)

	tx, err := CreateUndelegateTx(delegatorPubkey, validatorPubkey, amount, privKey, nonce)
	if err != nil {
		t.Fatalf("Failed to create undelegate tx: %v", err)
	}

	// Verify transaction fields
	if tx.Type != StakeTxTypeUndelegate {
		t.Errorf("Transaction type = %v, want %v", tx.Type, StakeTxTypeUndelegate)
	}
	if tx.DelegatorPubkey != delegatorPubkey {
		t.Errorf("Delegator pubkey = %v, want %v", tx.DelegatorPubkey, delegatorPubkey)
	}
	if tx.ValidatorPubkey != validatorPubkey {
		t.Errorf("Validator pubkey = %v, want %v", tx.ValidatorPubkey, validatorPubkey)
	}
	if tx.Amount.Cmp(amount) != 0 {
		t.Errorf("Amount = %v, want %v", tx.Amount, amount)
	}
	if tx.Nonce != nonce {
		t.Errorf("Nonce = %v, want %v", tx.Nonce, nonce)
	}

	// Verify signature
	processor := NewStakeTransactionProcessor(nil)
	if !processor.verifyStakeTransaction(tx, pubKey) {
		t.Errorf("Transaction signature verification failed")
	}
}

func TestStakeTxType_String(t *testing.T) {
	tests := []struct {
		txType   StakeTxType
		expected string
	}{
		{StakeTxTypeRegisterValidator, "register_validator"},
		{StakeTxTypeDelegate, "delegate"},
		{StakeTxTypeUndelegate, "undelegate"},
		{StakeTxTypeUpdateCommission, "update_commission"},
		{StakeTxTypeDeactivateValidator, "deactivate_validator"},
		{StakeTxType(999), "unknown"},
	}

	for _, tt := range tests {
		result := tt.txType.String()
		if result != tt.expected {
			t.Errorf("StakeTxType(%d).String() = %v, want %v", tt.txType, result, tt.expected)
		}
	}
}

func TestSerializeDeserializeTransaction(t *testing.T) {
	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	// Create original transaction
	original, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
	if err != nil {
		t.Fatalf("Failed to create transaction: %v", err)
	}

	// Serialize
	serialized, err := SerializeTransaction(original)
	if err != nil {
		t.Fatalf("Failed to serialize transaction: %v", err)
	}

	// Deserialize
	deserialized, err := DeserializeTransaction(serialized)
	if err != nil {
		t.Fatalf("Failed to deserialize transaction: %v", err)
	}

	// Compare fields
	if deserialized.Type != original.Type {
		t.Errorf("Type = %v, want %v", deserialized.Type, original.Type)
	}
	if deserialized.ValidatorPubkey != original.ValidatorPubkey {
		t.Errorf("ValidatorPubkey = %v, want %v", deserialized.ValidatorPubkey, original.ValidatorPubkey)
	}
	if deserialized.DelegatorPubkey != original.DelegatorPubkey {
		t.Errorf("DelegatorPubkey = %v, want %v", deserialized.DelegatorPubkey, original.DelegatorPubkey)
	}
	if deserialized.Amount.Cmp(original.Amount) != 0 {
		t.Errorf("Amount = %v, want %v", deserialized.Amount, original.Amount)
	}
	if deserialized.Commission != original.Commission {
		t.Errorf("Commission = %v, want %v", deserialized.Commission, original.Commission)
	}
	if deserialized.Nonce != original.Nonce {
		t.Errorf("Nonce = %v, want %v", deserialized.Nonce, original.Nonce)
	}
	if string(deserialized.Signature) != string(original.Signature) {
		t.Errorf("Signature mismatch")
	}
	if !deserialized.Timestamp.Equal(original.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", deserialized.Timestamp, original.Timestamp)
	}
}

func TestStakeTransactionProcessor_Nonces(t *testing.T) {
	processor := NewStakeTransactionProcessor(nil)
	pubkey := "test_pubkey"

	// Initial nonce should be 0
	if processor.GetNonce(pubkey) != 0 {
		t.Errorf("Initial nonce = %d, want 0", processor.GetNonce(pubkey))
	}

	// Manually increment nonce (simulating successful transaction)
	processor.nonces[pubkey] = 1
	if processor.GetNonce(pubkey) != 1 {
		t.Errorf("Updated nonce = %d, want 1", processor.GetNonce(pubkey))
	}

	// Test different pubkey
	pubkey2 := "test_pubkey2"
	if processor.GetNonce(pubkey2) != 0 {
		t.Errorf("Different pubkey nonce = %d, want 0", processor.GetNonce(pubkey2))
	}
}

func TestStakeTransactionProcessor_UpdateCommission(t *testing.T) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10, 8640)
	processor := NewStakeTransactionProcessor(stakePool)

	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	// Register validator first
	regTx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
	if err != nil {
		t.Fatalf("Failed to create register validator tx: %v", err)
	}
	err = processor.ProcessTransaction(regTx, pubKey)
	if err != nil {
		t.Fatalf("Failed to register validator: %v", err)
	}

	// Test update commission
	updateTx := &StakeTransaction{
		Type:            StakeTxTypeUpdateCommission,
		ValidatorPubkey: pubKeyStr,
		DelegatorPubkey: pubKeyStr,
		Commission:      10,
		Timestamp:       time.Now(),
		Nonce:           1,
	}

	signature, err := signStakeTransaction(updateTx, privKey)
	if err != nil {
		t.Fatalf("Failed to sign transaction: %v", err)
	}
	updateTx.Signature = signature

	err = processor.ProcessTransaction(updateTx, pubKey)
	if err != nil {
		t.Errorf("Failed to update commission: %v", err)
	}

	// Verify commission was updated
	validator, exists := stakePool.GetValidatorInfo(pubKeyStr)
	if !exists {
		t.Fatalf("Validator not found")
	}
	if validator.Commission != 10 {
		t.Errorf("Commission = %d, want 10", validator.Commission)
	}
}

func TestStakeTransactionProcessor_DeactivateValidator(t *testing.T) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10, 8640)
	processor := NewStakeTransactionProcessor(stakePool)

	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	// Register validator first
	regTx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
	if err != nil {
		t.Fatalf("Failed to create register validator tx: %v", err)
	}
	err = processor.ProcessTransaction(regTx, pubKey)
	if err != nil {
		t.Fatalf("Failed to register validator: %v", err)
	}

	// Test deactivate validator
	deactivateTx := &StakeTransaction{
		Type:            StakeTxTypeDeactivateValidator,
		ValidatorPubkey: pubKeyStr,
		DelegatorPubkey: pubKeyStr,
		Timestamp:       time.Now(),
		Nonce:           1,
	}

	signature, err := signStakeTransaction(deactivateTx, privKey)
	if err != nil {
		t.Fatalf("Failed to sign transaction: %v", err)
	}
	deactivateTx.Signature = signature

	err = processor.ProcessTransaction(deactivateTx, pubKey)
	if err != nil {
		t.Errorf("Failed to deactivate validator: %v", err)
	}

	// Verify validator was deactivated
	validator, exists := stakePool.GetValidatorInfo(pubKeyStr)
	if !exists {
		t.Fatalf("Validator not found")
	}
	if validator.State != StakeStateDeactivating {
		t.Errorf("State = %v, want %v", validator.State, StakeStateDeactivating)
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[len(s)-len(substr):] == substr || 
		   len(s) >= len(substr) && s[:len(substr)] == substr ||
		   (len(s) > len(substr) && 
		    func() bool {
			    for i := 0; i <= len(s)-len(substr); i++ {
				    if s[i:i+len(substr)] == substr {
					    return true
				    }
			    }
			    return false
		    }())
}

func BenchmarkCreateRegisterValidatorTx(b *testing.B) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, uint64(i))
		if err != nil {
			b.Fatalf("Failed to create transaction: %v", err)
		}
	}
}

func BenchmarkSerializeTransaction(b *testing.B) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
	if err != nil {
		b.Fatalf("Failed to create transaction: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := SerializeTransaction(tx)
		if err != nil {
			b.Fatalf("Failed to serialize transaction: %v", err)
		}
	}
}
