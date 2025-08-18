package staking

import (
	"testing"

	"github.com/mezonai/mmn/poh"
)

func TestStakeWeightedScheduler_GenerateLeaderSchedule(t *testing.T) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10, 8640)
	slotsPerEpoch := uint64(100) // Smaller for testing
	scheduler := NewStakeWeightedScheduler(stakePool, slotsPerEpoch)

	// Test with no validators
	_, err := scheduler.GenerateLeaderSchedule(0, []byte("test_seed"))
	if err == nil {
		t.Errorf("Expected error with no validators")
	}
	if err.Error() != "no active validators" {
		t.Errorf("Wrong error message: %v", err.Error())
	}

	// Register validators
	validators := []string{"validator1", "validator2", "validator3", "validator4", "validator5"}
	for _, pubkey := range validators {
		err := stakePool.RegisterValidator(pubkey, big.NewInt(10000000), 5)
		if err != nil {
			t.Fatalf("Failed to register validator %s: %v", pubkey, err)
		}
		// Set to active
		if validator, exists := stakePool.validators[pubkey]; exists {
			validator.State = StakeStateActive
		}
	}

	epoch := uint64(1)
	seed := []byte("test_seed")
	
	schedule, err := scheduler.GenerateLeaderSchedule(epoch, seed)
	if err != nil {
		t.Fatalf("Failed to generate leader schedule: %v", err)
	}

	// Verify schedule covers all slots in epoch
	epochStartSlot := epoch * slotsPerEpoch
	epochEndSlot := epochStartSlot + slotsPerEpoch - 1

	slotsAssigned := make(map[uint64]string)
	
	// Check each slot has a leader
	for slot := epochStartSlot; slot <= epochEndSlot; slot++ {
		leader, exists := schedule.LeaderAt(slot)
		if !exists {
			t.Errorf("No leader assigned for slot %d", slot)
			continue
		}
		slotsAssigned[slot] = leader
		
		// Verify leader is one of our validators
		found := false
		for _, validator := range validators {
			if leader == validator {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Unknown leader %s assigned to slot %d", leader, slot)
		}
	}

	// Verify equal distribution
	leaderCounts := make(map[string]int)
	for _, leader := range slotsAssigned {
		leaderCounts[leader]++
	}

	expectedSlotsPerValidator := int(slotsPerEpoch) / len(validators)
	tolerance := 2 // Allow small variations due to rounding

	for _, validator := range validators {
		count := leaderCounts[validator]
		if count < expectedSlotsPerValidator-tolerance || count > expectedSlotsPerValidator+tolerance {
			t.Errorf("Validator %s assigned %d slots, expected ~%d", validator, count, expectedSlotsPerValidator)
		}
	}

	// Verify total slots assigned
	totalAssigned := 0
	for _, count := range leaderCounts {
		totalAssigned += count
	}
	if totalAssigned != int(slotsPerEpoch) {
		t.Errorf("Total slots assigned = %d, want %d", totalAssigned, slotsPerEpoch)
	}
}

func TestStakeWeightedScheduler_GenerateRandomizedSchedule(t *testing.T) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10, 8640)
	slotsPerEpoch := uint64(60) // 60 slots for testing
	scheduler := NewStakeWeightedScheduler(stakePool, slotsPerEpoch)

	// Register 3 validators
	validators := []string{"validator1", "validator2", "validator3"}
	for _, pubkey := range validators {
		err := stakePool.RegisterValidator(pubkey, big.NewInt(10000000), 5)
		if err != nil {
			t.Fatalf("Failed to register validator %s: %v", pubkey, err)
		}
		// Set to active
		if validator, exists := stakePool.validators[pubkey]; exists {
			validator.State = StakeStateActive
		}
	}

	epoch := uint64(1)
	seed := []byte("test_seed")
	
	schedule, err := scheduler.GenerateRandomizedSchedule(epoch, seed)
	if err != nil {
		t.Fatalf("Failed to generate randomized schedule: %v", err)
	}

	// Verify each slot has exactly one leader
	epochStartSlot := epoch * slotsPerEpoch
	leaderCounts := make(map[string]int)
	
	for slot := epochStartSlot; slot < epochStartSlot+slotsPerEpoch; slot++ {
		leader, exists := schedule.LeaderAt(slot)
		if !exists {
			t.Errorf("No leader assigned for slot %d", slot)
			continue
		}
		leaderCounts[leader]++
	}

	// Verify round-robin distribution (should be very close to equal)
	expectedCount := int(slotsPerEpoch) / len(validators)
	for _, validator := range validators {
		count := leaderCounts[validator]
		if count < expectedCount || count > expectedCount+1 {
			t.Errorf("Validator %s assigned %d slots, expected %d or %d", validator, count, expectedCount, expectedCount+1)
		}
	}

	// Test determinism - same epoch and seed should produce same schedule
	schedule2, err := scheduler.GenerateRandomizedSchedule(epoch, seed)
	if err != nil {
		t.Fatalf("Failed to generate second schedule: %v", err)
	}

	for slot := epochStartSlot; slot < epochStartSlot+slotsPerEpoch; slot++ {
		leader1, _ := schedule.LeaderAt(slot)
		leader2, _ := schedule2.LeaderAt(slot)
		if leader1 != leader2 {
			t.Errorf("Non-deterministic schedule: slot %d has different leaders %s vs %s", slot, leader1, leader2)
		}
	}

	// Test different seeds produce different schedules
	differentSeed := []byte("different_seed")
	schedule3, err := scheduler.GenerateRandomizedSchedule(epoch, differentSeed)
	if err != nil {
		t.Fatalf("Failed to generate third schedule: %v", err)
	}

	differenceCount := 0
	for slot := epochStartSlot; slot < epochStartSlot+slotsPerEpoch; slot++ {
		leader1, _ := schedule.LeaderAt(slot)
		leader3, _ := schedule3.LeaderAt(slot)
		if leader1 != leader3 {
			differenceCount++
		}
	}

	// Should have significant differences with different seeds
	minDifferences := int(slotsPerEpoch) / 4 // At least 25% different
	if differenceCount < minDifferences {
		t.Errorf("Too few differences between different seeds: %d, expected at least %d", differenceCount, minDifferences)
	}
}

func TestStakeWeightedScheduler_ValidateLeaderSchedule(t *testing.T) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10, 8640)
	slotsPerEpoch := uint64(50)
	scheduler := NewStakeWeightedScheduler(stakePool, slotsPerEpoch)

	// Register validators
	validators := []string{"validator1", "validator2"}
	for _, pubkey := range validators {
		err := stakePool.RegisterValidator(pubkey, big.NewInt(10000000), 5)
		if err != nil {
			t.Fatalf("Failed to register validator %s: %v", pubkey, err)
		}
		// Set to active
		if validator, exists := stakePool.validators[pubkey]; exists {
			validator.State = StakeStateActive
		}
	}

	epoch := uint64(2)
	seed := []byte("validation_seed")

	// Generate correct schedule
	correctSchedule, err := scheduler.GenerateLeaderSchedule(epoch, seed)
	if err != nil {
		t.Fatalf("Failed to generate correct schedule: %v", err)
	}

	// Validate correct schedule
	err = scheduler.ValidateLeaderSchedule(correctSchedule, epoch, seed)
	if err != nil {
		t.Errorf("Validation failed for correct schedule: %v", err)
	}

	// Create incorrect schedule
	entries := []poh.LeaderScheduleEntry{
		{
			StartSlot: epoch * slotsPerEpoch,
			EndSlot:   epoch * slotsPerEpoch + 10,
			Leader:    "wrong_leader",
		},
	}
	incorrectSchedule, err := poh.NewLeaderSchedule(entries)
	if err != nil {
		t.Fatalf("Failed to create incorrect schedule: %v", err)
	}

	// Validate incorrect schedule
	err = scheduler.ValidateLeaderSchedule(incorrectSchedule, epoch, seed)
	if err == nil {
		t.Errorf("Expected validation error for incorrect schedule")
	}
}

func TestStakeWeightedScheduler_EpochCalculations(t *testing.T) {
	slotsPerEpoch := uint64(100)
	scheduler := &StakeWeightedScheduler{slotsPerEpoch: slotsPerEpoch}

	tests := []struct {
		slot        uint64
		expectedEpoch uint64
	}{
		{0, 0},
		{50, 0},
		{99, 0},
		{100, 1},
		{150, 1},
		{200, 2},
		{999, 9},
		{1000, 10},
	}

	for _, tt := range tests {
		epoch := scheduler.GetEpochFromSlot(tt.slot)
		if epoch != tt.expectedEpoch {
			t.Errorf("GetEpochFromSlot(%d) = %d, want %d", tt.slot, epoch, tt.expectedEpoch)
		}
	}

	// Test epoch start/end calculations
	epochTests := []struct {
		epoch     uint64
		startSlot uint64
		endSlot   uint64
	}{
		{0, 0, 99},
		{1, 100, 199},
		{2, 200, 299},
		{10, 1000, 1099},
	}

	for _, tt := range epochTests {
		startSlot := scheduler.GetEpochStartSlot(tt.epoch)
		if startSlot != tt.startSlot {
			t.Errorf("GetEpochStartSlot(%d) = %d, want %d", tt.epoch, startSlot, tt.startSlot)
		}

		endSlot := scheduler.GetEpochEndSlot(tt.epoch)
		if endSlot != tt.endSlot {
			t.Errorf("GetEpochEndSlot(%d) = %d, want %d", tt.epoch, endSlot, tt.endSlot)
		}
	}
}

func TestStakeWeightedScheduler_CalculateStakeRewards(t *testing.T) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10, 8640)
	scheduler := NewStakeWeightedScheduler(stakePool, 8640)

	// Register validators
	validators := []string{"validator1", "validator2", "validator3"}
	for _, pubkey := range validators {
		err := stakePool.RegisterValidator(pubkey, big.NewInt(10000000), 5)
		if err != nil {
			t.Fatalf("Failed to register validator %s: %v", pubkey, err)
		}
		// Set to active
		if validator, exists := stakePool.validators[pubkey]; exists {
			validator.State = StakeStateActive
		}
	}

	epoch := uint64(1)
	totalRewards := big.NewInt(300000000) // 300M tokens
	
	// Test without performance metrics (equal distribution)
	rewards := scheduler.CalculateStakeRewards(epoch, totalRewards, nil)
	
	expectedRewardPerValidator := big.NewInt(100000000) // 100M per validator
	for _, validator := range validators {
		reward := rewards[validator]
		if reward == nil {
			t.Errorf("Validator %s received no reward", validator)
			continue
		}
		if reward.Cmp(expectedRewardPerValidator) != 0 {
			t.Errorf("Validator %s reward = %v, want %v", validator, reward, expectedRewardPerValidator)
		}
	}

	// Test with performance metrics
	performanceMetrics := map[string]float64{
		"validator1": 1.0,  // Perfect performance
		"validator2": 0.8,  // 80% performance
		"validator3": 0.5,  // 50% performance
	}
	
	rewardsWithPerformance := scheduler.CalculateStakeRewards(epoch, totalRewards, performanceMetrics)
	
	for validator, performance := range performanceMetrics {
		baseReward := big.NewInt(100000000)
		expectedReward := new(big.Int).Mul(baseReward, big.NewInt(int64(performance*1000)))
		expectedReward.Div(expectedReward, big.NewInt(1000))
		
		actualReward := rewardsWithPerformance[validator]
		if actualReward == nil {
			t.Errorf("Validator %s received no performance-based reward", validator)
			continue
		}
		if actualReward.Cmp(expectedReward) != 0 {
			t.Errorf("Validator %s performance reward = %v, want %v", validator, actualReward, expectedReward)
		}
	}
}

func BenchmarkScheduler_GenerateLeaderSchedule(b *testing.B) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 1000, 8640)
	scheduler := NewStakeWeightedScheduler(stakePool, 8640)

	// Register 100 validators
	for i := 0; i < 100; i++ {
		pubkey := fmt.Sprintf("validator%d", i)
		stakePool.RegisterValidator(pubkey, big.NewInt(10000000), 5)
		if validator, exists := stakePool.validators[pubkey]; exists {
			validator.State = StakeStateActive
		}
	}

	seed := []byte("benchmark_seed")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scheduler.GenerateLeaderSchedule(uint64(i), seed)
		if err != nil {
			b.Fatalf("Failed to generate schedule: %v", err)
		}
	}
}

func BenchmarkScheduler_GenerateRandomizedSchedule(b *testing.B) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 1000, 8640)
	scheduler := NewStakeWeightedScheduler(stakePool, 8640)

	// Register 50 validators
	for i := 0; i < 50; i++ {
		pubkey := fmt.Sprintf("validator%d", i)
		stakePool.RegisterValidator(pubkey, big.NewInt(10000000), 5)
		if validator, exists := stakePool.validators[pubkey]; exists {
			validator.State = StakeStateActive
		}
	}

	seed := []byte("benchmark_seed")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scheduler.GenerateRandomizedSchedule(uint64(i), seed)
		if err != nil {
			b.Fatalf("Failed to generate randomized schedule: %v", err)
		}
	}
}
