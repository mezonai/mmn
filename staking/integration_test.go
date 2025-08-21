package staking

import (
	"crypto/ed25519"
	"crypto/rand"
	"math/big"
	"testing"
	"time"

	"github.com/mezonai/mmn/config"
)

// Integration test for the complete staking system
func TestStakingIntegration_CompleteFlow(t *testing.T) {
	// Initialize core components
	cfg := &config.Config{
		StakingEnabled: true,
		MinStake:       big.NewInt(1000000),
		MaxValidators:  10,
		EpochLength:    8640,
	}

	// Create mock services
	pohService := &mockPoHService{}
	ledgerService := &mockLedger{}
	mempoolService := &mockMempool{}
	networkService := &mockNetwork{}

	// Initialize staking system
	stakeManager := NewStakeManager(pohService, ledgerService, mempoolService, networkService)
	err := stakeManager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize stake manager: %v", err)
	}

	// Test Phase 1: Register validators with equal distribution requirement
	numValidators := 10
	validators := make([]ValidatorSetup, numValidators)

	for i := 0; i < numValidators; i++ {
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate key for validator %d: %v", i, err)
		}

		validators[i] = ValidatorSetup{
			PubKey:      pubKey,
			PrivKey:     privKey,
			PubKeyStr:   string(pubKey),
			StakeAmount: big.NewInt(10000000), // All validators have equal stake
			Commission:  uint32(5),            // 5% commission for all
		}

		// Register validator
		tx, err := CreateRegisterValidatorTx(
			validators[i].PubKeyStr,
			validators[i].StakeAmount,
			validators[i].Commission,
			validators[i].PrivKey,
			0,
		)
		if err != nil {
			t.Fatalf("Failed to create register validator tx for validator %d: %v", i, err)
		}

		err = stakeManager.ProcessStakeTransaction(tx, validators[i].PubKey)
		if err != nil {
			t.Fatalf("Failed to process register validator tx for validator %d: %v", i, err)
		}
	}

	// Verify all validators are registered
	validatorList := stakeManager.GetValidatorList()
	if len(validatorList) != numValidators {
		t.Errorf("Expected %d validators, got %d", numValidators, len(validatorList))
	}

	// Test Phase 2: Verify equal distribution (10 validators = 10% each)
	distribution := stakeManager.GetStakeDistribution()
	expectedWeight := 1.0 / float64(numValidators) // 0.1 for each validator
	tolerance := 0.001

	for validator, weight := range distribution {
		if weight < expectedWeight-tolerance || weight > expectedWeight+tolerance {
			t.Errorf("Validator %s has weight %f, expected %f (10%%)", validator, weight, expectedWeight)
		}
	}

	// Verify total weight is 1.0
	var totalWeight float64
	for _, weight := range distribution {
		totalWeight += weight
	}
	if totalWeight < 0.999 || totalWeight > 1.001 {
		t.Errorf("Total weight = %f, expected 1.0", totalWeight)
	}

	// Test Phase 3: Start epoch and verify leader schedule
	epoch := uint64(1)
	err = stakeManager.StartEpoch(epoch)
	if err != nil {
		t.Fatalf("Failed to start epoch %d: %v", epoch, err)
	}

	// Verify leader schedule has equal distribution
	schedule := stakeManager.scheduler.GetCurrentSchedule()
	if len(schedule) == 0 {
		t.Fatal("Leader schedule should not be empty")
	}

	// Count slots per validator
	slotCount := make(map[string]int)
	for _, leader := range schedule {
		slotCount[leader]++
	}

	// Each validator should have approximately equal slots
	expectedSlotsPerValidator := len(schedule) / numValidators
	for validator, count := range slotCount {
		if count < expectedSlotsPerValidator || count > expectedSlotsPerValidator+1 {
			t.Errorf("Validator %s has %d slots, expected around %d", validator, count, expectedSlotsPerValidator)
		}
	}

	// Test Phase 4: Process rewards with equal distribution
	totalRewards := big.NewInt(10000000) // 10M tokens reward
	err = stakeManager.ProcessRewards(totalRewards)
	if err != nil {
		t.Fatalf("Failed to process rewards: %v", err)
	}

	// Verify each validator received equal rewards (minus commission)
	expectedRewardPerValidator := new(big.Int).Div(totalRewards, big.NewInt(int64(numValidators)))
	for _, validator := range validators {
		info, exists := stakeManager.stakePool.GetValidatorInfo(validator.PubKeyStr)
		if !exists {
			t.Errorf("Validator %s not found", validator.PubKeyStr)
			continue
		}

		// Calculate expected reward after commission (5%)
		expectedAfterCommission := new(big.Int).Mul(expectedRewardPerValidator, big.NewInt(95))
		expectedAfterCommission.Div(expectedAfterCommission, big.NewInt(100))

		if info.AccumulatedRewards.Cmp(expectedAfterCommission) != 0 {
			t.Errorf("Validator %s accumulated rewards = %v, expected %v",
				validator.PubKeyStr, info.AccumulatedRewards, expectedAfterCommission)
		}
	}

	// Test Phase 5: Delegation to validators
	delegatorPubKey, delegatorPrivKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate delegator key: %v", err)
	}
	delegatorPubKeyStr := string(delegatorPubKey)

	// Delegate to first validator
	delegateAmount := big.NewInt(5000000)
	delegateTx, err := CreateDelegateTx(
		delegatorPubKeyStr,
		validators[0].PubKeyStr,
		delegateAmount,
		delegatorPrivKey,
		0,
	)
	if err != nil {
		t.Fatalf("Failed to create delegate tx: %v", err)
	}

	err = stakeManager.ProcessStakeTransaction(delegateTx, delegatorPubKey)
	if err != nil {
		t.Fatalf("Failed to process delegate tx: %v", err)
	}

	// Verify delegation
	validator0Info, exists := stakeManager.stakePool.GetValidatorInfo(validators[0].PubKeyStr)
	if !exists {
		t.Fatal("Validator 0 not found")
	}
	if len(validator0Info.Delegators) != 1 {
		t.Errorf("Expected 1 delegator, got %d", len(validator0Info.Delegators))
	}

	// Test Phase 6: Update commission
	newCommission := uint32(8)
	updateCommissionTx := &StakeTransaction{
		Type:            StakeTxTypeUpdateCommission,
		ValidatorPubkey: validators[1].PubKeyStr,
		DelegatorPubkey: validators[1].PubKeyStr,
		Commission:      newCommission,
		Timestamp:       time.Now(),
		Nonce:           1,
	}

	signature, err := signStakeTransaction(updateCommissionTx, validators[1].PrivKey)
	if err != nil {
		t.Fatalf("Failed to sign update commission tx: %v", err)
	}
	updateCommissionTx.Signature = signature

	err = stakeManager.ProcessStakeTransaction(updateCommissionTx, validators[1].PubKey)
	if err != nil {
		t.Fatalf("Failed to process update commission tx: %v", err)
	}

	// Verify commission update
	validator1Info, exists := stakeManager.stakePool.GetValidatorInfo(validators[1].PubKeyStr)
	if !exists {
		t.Fatal("Validator 1 not found")
	}
	if validator1Info.Commission != newCommission {
		t.Errorf("Commission = %d, expected %d", validator1Info.Commission, newCommission)
	}

	// Test Phase 7: Multiple epochs with consistent equal distribution
	for epochNum := uint64(2); epochNum <= 5; epochNum++ {
		err = stakeManager.StartEpoch(epochNum)
		if err != nil {
			t.Fatalf("Failed to start epoch %d: %v", epochNum, err)
		}

		// Verify equal distribution is maintained
		distribution = stakeManager.GetStakeDistribution()
		for validator, weight := range distribution {
			// With delegation, validator[0] will have slightly more weight
			if validator == validators[0].PubKeyStr {
				// Original stake (10M) + delegation (5M) = 15M out of total 105M
				expectedWeightWithDelegation := 15.0 / 105.0
				if weight < expectedWeightWithDelegation-tolerance || weight > expectedWeightWithDelegation+tolerance {
					t.Errorf("Epoch %d: Validator %s with delegation has weight %f, expected %f",
						epochNum, validator, weight, expectedWeightWithDelegation)
				}
			} else {
				// Other validators: 10M out of total 105M
				expectedWeightNormal := 10.0 / 105.0
				if weight < expectedWeightNormal-tolerance || weight > expectedWeightNormal+tolerance {
					t.Errorf("Epoch %d: Validator %s has weight %f, expected %f",
						epochNum, validator, weight, expectedWeightNormal)
				}
			}
		}
	}

	// Test Phase 8: Deactivate validator
	deactivateTx := &StakeTransaction{
		Type:            StakeTxTypeDeactivateValidator,
		ValidatorPubkey: validators[9].PubKeyStr, // Deactivate last validator
		DelegatorPubkey: validators[9].PubKeyStr,
		Timestamp:       time.Now(),
		Nonce:           1,
	}

	signature, err = signStakeTransaction(deactivateTx, validators[9].PrivKey)
	if err != nil {
		t.Fatalf("Failed to sign deactivate tx: %v", err)
	}
	deactivateTx.Signature = signature

	err = stakeManager.ProcessStakeTransaction(deactivateTx, validators[9].PubKey)
	if err != nil {
		t.Fatalf("Failed to process deactivate tx: %v", err)
	}

	// Verify validator is deactivating
	validator9Info, exists := stakeManager.stakePool.GetValidatorInfo(validators[9].PubKeyStr)
	if !exists {
		t.Fatal("Validator 9 not found")
	}
	if validator9Info.State != StakeStateDeactivating {
		t.Errorf("Validator state = %v, expected %v", validator9Info.State, StakeStateDeactivating)
	}

	// Start new epoch and verify deactivated validator is not in schedule
	err = stakeManager.StartEpoch(6)
	if err != nil {
		t.Fatalf("Failed to start epoch 6: %v", err)
	}

	schedule = stakeManager.scheduler.GetCurrentSchedule()
	for _, leader := range schedule {
		if leader == validators[9].PubKeyStr {
			t.Error("Deactivated validator should not be in leader schedule")
		}
	}

	// Final verification: Now 9 active validators should have equal distribution
	activeValidators := stakeManager.GetValidatorList()
	expectedActiveValidators := 9 // 10 - 1 deactivated
	if len(activeValidators) != expectedActiveValidators {
		t.Errorf("Expected %d active validators, got %d", expectedActiveValidators, len(activeValidators))
	}
}

func TestStakingIntegration_PerformanceUnderLoad(t *testing.T) {
	// Test system performance with maximum validators and heavy transaction load
	stakeManager := NewStakeManager(
		&mockPoHService{},
		&mockLedger{},
		&mockMempool{},
		&mockNetwork{},
	)
	err := stakeManager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize stake manager: %v", err)
	}

	// Register maximum validators (10)
	validators := make([]ValidatorSetup, 10)
	for i := 0; i < 10; i++ {
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate key for validator %d: %v", i, err)
		}

		validators[i] = ValidatorSetup{
			PubKey:      pubKey,
			PrivKey:     privKey,
			PubKeyStr:   string(pubKey),
			StakeAmount: big.NewInt(10000000),
			Commission:  uint32(5),
		}

		tx, err := CreateRegisterValidatorTx(
			validators[i].PubKeyStr,
			validators[i].StakeAmount,
			validators[i].Commission,
			validators[i].PrivKey,
			0,
		)
		if err != nil {
			t.Fatalf("Failed to create register validator tx: %v", err)
		}

		err = stakeManager.ProcessStakeTransaction(tx, validators[i].PubKey)
		if err != nil {
			t.Fatalf("Failed to process register validator tx: %v", err)
		}
	}

	// Process many delegation transactions
	numDelegations := 100
	for i := 0; i < numDelegations; i++ {
		delegatorPubKey, delegatorPrivKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate delegator key %d: %v", i, err)
		}

		// Random validator to delegate to
		validatorIndex := i % len(validators)
		delegateTx, err := CreateDelegateTx(
			string(delegatorPubKey),
			validators[validatorIndex].PubKeyStr,
			big.NewInt(1000000),
			delegatorPrivKey,
			0,
		)
		if err != nil {
			t.Fatalf("Failed to create delegate tx %d: %v", i, err)
		}

		err = stakeManager.ProcessStakeTransaction(delegateTx, delegatorPubKey)
		if err != nil {
			t.Fatalf("Failed to process delegate tx %d: %v", i, err)
		}
	}

	// Process multiple epochs quickly
	for epoch := uint64(1); epoch <= 10; epoch++ {
		start := time.Now()
		err = stakeManager.StartEpoch(epoch)
		if err != nil {
			t.Fatalf("Failed to start epoch %d: %v", epoch, err)
		}
		duration := time.Since(start)

		// Epoch transitions should be fast (under 100ms)
		if duration > 100*time.Millisecond {
			t.Errorf("Epoch %d transition took %v, expected < 100ms", epoch, duration)
		}
	}

	// Verify system maintains equal distribution under load
	distribution := stakeManager.GetStakeDistribution()
	if len(distribution) != 10 {
		t.Errorf("Expected 10 validators in distribution, got %d", len(distribution))
	}

	// Each validator should have roughly equal weight considering delegations
	totalStake := big.NewInt(100000000) // 10 validators * 10M each + 100 delegations * 1M each
	for validator, weight := range distribution {
		// Find validator's total stake (own + delegations)
		validatorInfo, exists := stakeManager.stakePool.GetValidatorInfo(validator)
		if !exists {
			t.Errorf("Validator %s not found", validator)
			continue
		}

		expectedWeight := float64(validatorInfo.StakeAmount.Int64()) / float64(totalStake.Int64())
		tolerance := 0.05 // 5% tolerance for performance test

		if weight < expectedWeight-tolerance || weight > expectedWeight+tolerance {
			t.Errorf("Validator %s weight %f deviates from expected %f beyond tolerance",
				validator, weight, expectedWeight)
		}
	}
}

// Helper struct for test setup
type ValidatorSetup struct {
	PubKey      ed25519.PublicKey
	PrivKey     ed25519.PrivateKey
	PubKeyStr   string
	StakeAmount *big.Int
	Commission  uint32
}

func BenchmarkStakingIntegration_FullCycle(b *testing.B) {
	stakeManager := NewStakeManager(
		&mockPoHService{},
		&mockLedger{},
		&mockMempool{},
		&mockNetwork{},
	)
	stakeManager.Initialize()

	// Pre-register validators
	validators := make([]ValidatorSetup, 10)
	for i := 0; i < 10; i++ {
		pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
		validators[i] = ValidatorSetup{
			PubKey:      pubKey,
			PrivKey:     privKey,
			PubKeyStr:   string(pubKey),
			StakeAmount: big.NewInt(10000000),
			Commission:  uint32(5),
		}

		tx, _ := CreateRegisterValidatorTx(
			validators[i].PubKeyStr,
			validators[i].StakeAmount,
			validators[i].Commission,
			validators[i].PrivKey,
			0,
		)
		stakeManager.ProcessStakeTransaction(tx, validators[i].PubKey)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Benchmark full epoch cycle
		err := stakeManager.StartEpoch(uint64(i))
		if err != nil {
			b.Fatalf("Failed to start epoch %d: %v", i, err)
		}

		// Process some rewards
		totalRewards := big.NewInt(1000000)
		err = stakeManager.ProcessRewards(totalRewards)
		if err != nil {
			b.Fatalf("Failed to process rewards in epoch %d: %v", i, err)
		}
	}
}
