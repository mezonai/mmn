package staking

import (
	"crypto/ed25519"
	"crypto/rand"
	"math/big"
	"testing"
)

// Simple standalone test for core staking functionality
func TestStakePoolBasic(t *testing.T) {
	// Test equal distribution requirement: 10 validators = 10% each
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10, 8640)

	// Register 10 validators with equal stake (hard-coded amounts)
	validators := make([]string, 10)
	for i := 0; i < 10; i++ {
		pubKey, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate key for validator %d: %v", i, err)
		}
		
		pubKeyStr := string(pubKey)
		validators[i] = pubKeyStr
		
		// Hard-coded stake amount: 10M tokens for each validator
		stakeAmount := big.NewInt(10000000)
		commission := uint32(5) // 5% commission
		
		err = stakePool.RegisterValidator(pubKeyStr, stakeAmount, commission)
		if err != nil {
			t.Fatalf("Failed to register validator %d: %v", i, err)
		}
	}

	// Verify equal distribution: each validator should have 1/10 = 0.1 (10%)
	distribution := stakePool.GetStakeDistribution()
	
	if len(distribution) != 10 {
		t.Errorf("Expected 10 validators, got %d", len(distribution))
	}

	expectedWeight := 0.1 // 10% for each validator
	tolerance := 0.001
	
	var totalWeight float64
	for validator, weight := range distribution {
		if weight < expectedWeight-tolerance || weight > expectedWeight+tolerance {
			t.Errorf("Validator %s has weight %f, expected %f (10%%)", validator, weight, expectedWeight)
		}
		totalWeight += weight
	}

	// Total weight should be 1.0
	if totalWeight < 0.999 || totalWeight > 1.001 {
		t.Errorf("Total weight = %f, expected 1.0", totalWeight)
	}

	t.Logf("✅ Equal Distribution Test PASSED")
	t.Logf("   - 10 validators registered")
	t.Logf("   - Each validator has exactly 10%% stake weight")
	t.Logf("   - Total weight = %.3f", totalWeight)
	t.Logf("   - Hard-coded stake amounts: 10M tokens per validator")
}

func TestSchedulerBasic(t *testing.T) {
	// Test equal slot distribution in leader schedule
	scheduler := NewStakeWeightedScheduler(8640) // Default epoch length
	
	// Create 10 validators with equal weights (10% each)
	stakeWeights := make(map[string]float64)
	validators := make([]string, 10)
	
	for i := 0; i < 10; i++ {
		pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
		validator := string(pubKey)
		validators[i] = validator
		stakeWeights[validator] = 0.1 // 10% each
	}

	// Generate leader schedule
	schedule, err := scheduler.GenerateLeaderSchedule(stakeWeights, 1000) // 1000 slots
	if err != nil {
		t.Fatalf("Failed to generate leader schedule: %v", err)
	}

	if len(schedule) != 1000 {
		t.Errorf("Expected 1000 slots, got %d", len(schedule))
	}

	// Count slots per validator - should be approximately equal
	slotCount := make(map[string]int)
	for _, leader := range schedule {
		slotCount[leader]++
	}

	expectedSlotsPerValidator := 100 // 1000 slots / 10 validators
	tolerance := 10 // Allow some variance

	for validator, count := range slotCount {
		if count < expectedSlotsPerValidator-tolerance || count > expectedSlotsPerValidator+tolerance {
			t.Errorf("Validator %s has %d slots, expected around %d", validator, count, expectedSlotsPerValidator)
		}
	}

	t.Logf("✅ Equal Slot Distribution Test PASSED")
	t.Logf("   - 1000 slots distributed among 10 validators")
	t.Logf("   - Each validator gets ~100 slots (10%%)")
	for validator, count := range slotCount {
		t.Logf("   - %s: %d slots", validator[:8]+"...", count)
	}
}

func TestRewardsDistribution(t *testing.T) {
	// Test reward distribution with equal stake
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10, 8640)

	// Register 5 validators for simpler testing
	validators := make([]string, 5)
	for i := 0; i < 5; i++ {
		pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
		pubKeyStr := string(pubKey)
		validators[i] = pubKeyStr
		
		// Equal stake: 10M tokens, 5% commission
		stakePool.RegisterValidator(pubKeyStr, big.NewInt(10000000), 5)
	}

	// Distribute 5M tokens as rewards
	totalRewards := big.NewInt(5000000)
	rewardData := make(map[string]*big.Int)
	
	// Equal rewards for equal stake
	rewardPerValidator := new(big.Int).Div(totalRewards, big.NewInt(5))
	for _, validator := range validators {
		rewardData[validator] = rewardPerValidator
	}

	err := stakePool.DistributeRewards(rewardData)
	if err != nil {
		t.Fatalf("Failed to distribute rewards: %v", err)
	}

	// Verify each validator received correct rewards (minus 5% commission)
	expectedReward := new(big.Int).Mul(rewardPerValidator, big.NewInt(95))
	expectedReward.Div(expectedReward, big.NewInt(100)) // 95% after commission

	for _, validator := range validators {
		info, exists := stakePool.GetValidatorInfo(validator)
		if !exists {
			t.Errorf("Validator %s not found", validator)
			continue
		}

		if info.AccumulatedRewards.Cmp(expectedReward) != 0 {
			t.Errorf("Validator %s rewards = %v, expected %v", 
				validator, info.AccumulatedRewards, expectedReward)
		}
	}

	t.Logf("✅ Rewards Distribution Test PASSED")
	t.Logf("   - 5M tokens distributed equally among 5 validators")
	t.Logf("   - Each validator gets 950K tokens (1M - 5%% commission)")
}

func BenchmarkStakePoolOperations(b *testing.B) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10, 8640)

	// Pre-register validators
	validators := make([]string, 10)
	for i := 0; i < 10; i++ {
		pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
		validators[i] = string(pubKey)
		stakePool.RegisterValidator(validators[i], big.NewInt(10000000), 5)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Benchmark getting stake distribution
		stakePool.GetStakeDistribution()
	}
}
