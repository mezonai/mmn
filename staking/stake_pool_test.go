package staking

import (
	"math/big"
	"testing"
	"time"
)

func TestStakePool_RegisterValidator(t *testing.T) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10, 8640)

	tests := []struct {
		name        string
		pubkey      string
		stakeAmount *big.Int
		commission  uint8
		wantErr     bool
		errMsg      string
	}{
		{
			name:        "Valid validator registration",
			pubkey:      "validator1",
			stakeAmount: big.NewInt(10000000),
			commission:  5,
			wantErr:     false,
		},
		{
			name:        "Stake below minimum",
			pubkey:      "validator2",
			stakeAmount: big.NewInt(500000),
			commission:  5,
			wantErr:     true,
			errMsg:      "stake amount 500000 is below minimum 1000000",
		},
		{
			name:        "Commission too high",
			pubkey:      "validator3",
			stakeAmount: big.NewInt(10000000),
			commission:  101,
			wantErr:     true,
			errMsg:      "commission cannot exceed 100%",
		},
		{
			name:        "Duplicate validator",
			pubkey:      "validator1", // Same as first test
			stakeAmount: big.NewInt(10000000),
			commission:  5,
			wantErr:     true,
			errMsg:      "validator already registered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := stakePool.RegisterValidator(tt.pubkey, tt.stakeAmount, tt.commission)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("RegisterValidator() expected error but got none")
					return
				}
				if err.Error() != tt.errMsg {
					t.Errorf("RegisterValidator() error = %v, want %v", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("RegisterValidator() unexpected error = %v", err)
					return
				}
				
				// Verify validator was registered
				validator, exists := stakePool.GetValidatorInfo(tt.pubkey)
				if !exists {
					t.Errorf("Validator %s was not registered", tt.pubkey)
					return
				}
				
				if validator.Pubkey != tt.pubkey {
					t.Errorf("Validator pubkey = %v, want %v", validator.Pubkey, tt.pubkey)
				}
				if validator.StakeAmount.Cmp(tt.stakeAmount) != 0 {
					t.Errorf("Validator stake = %v, want %v", validator.StakeAmount, tt.stakeAmount)
				}
				if validator.Commission != tt.commission {
					t.Errorf("Validator commission = %v, want %v", validator.Commission, tt.commission)
				}
				if validator.State != StakeStateActivating {
					t.Errorf("Validator state = %v, want %v", validator.State, StakeStateActivating)
				}
			}
		})
	}
}

func TestStakePool_Delegate(t *testing.T) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10, 8640)
	
	// Register a validator first
	validatorPubkey := "validator1"
	err := stakePool.RegisterValidator(validatorPubkey, big.NewInt(10000000), 5)
	if err != nil {
		t.Fatalf("Failed to register validator: %v", err)
	}

	tests := []struct {
		name            string
		delegatorPubkey string
		validatorPubkey string
		amount          *big.Int
		wantErr         bool
		errMsg          string
	}{
		{
			name:            "Valid delegation",
			delegatorPubkey: "delegator1",
			validatorPubkey: validatorPubkey,
			amount:          big.NewInt(5000000),
			wantErr:         false,
		},
		{
			name:            "Additional delegation from same delegator",
			delegatorPubkey: "delegator1", // Same delegator
			validatorPubkey: validatorPubkey,
			amount:          big.NewInt(2000000),
			wantErr:         false,
		},
		{
			name:            "Delegation to non-existent validator",
			delegatorPubkey: "delegator2",
			validatorPubkey: "nonexistent",
			amount:          big.NewInt(1000000),
			wantErr:         true,
			errMsg:          "validator not found",
		},
		{
			name:            "Zero delegation amount",
			delegatorPubkey: "delegator3",
			validatorPubkey: validatorPubkey,
			amount:          big.NewInt(0),
			wantErr:         true,
			errMsg:          "delegation amount must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get initial state
			initialValidator, _ := stakePool.GetValidatorInfo(tt.validatorPubkey)
			var initialDelegation *big.Int
			if initialValidator != nil {
				if delegation, exists := initialValidator.Delegators[tt.delegatorPubkey]; exists {
					initialDelegation = new(big.Int).Set(delegation)
				} else {
					initialDelegation = big.NewInt(0)
				}
			}
			
			err := stakePool.Delegate(tt.delegatorPubkey, tt.validatorPubkey, tt.amount)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("Delegate() expected error but got none")
					return
				}
				if err.Error() != tt.errMsg {
					t.Errorf("Delegate() error = %v, want %v", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Delegate() unexpected error = %v", err)
					return
				}
				
				// Verify delegation was added
				validator, exists := stakePool.GetValidatorInfo(tt.validatorPubkey)
				if !exists {
					t.Errorf("Validator %s not found", tt.validatorPubkey)
					return
				}
				
				expectedDelegation := new(big.Int).Add(initialDelegation, tt.amount)
				actualDelegation := validator.Delegators[tt.delegatorPubkey]
				if actualDelegation.Cmp(expectedDelegation) != 0 {
					t.Errorf("Delegation amount = %v, want %v", actualDelegation, expectedDelegation)
				}
			}
		})
	}
}

func TestStakePool_GetStakeDistribution(t *testing.T) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10, 8640)

	// Test empty pool
	distribution := stakePool.GetStakeDistribution()
	if len(distribution) != 0 {
		t.Errorf("Empty pool should have no distribution, got %d validators", len(distribution))
	}

	// Register validators
	validators := []string{"validator1", "validator2", "validator3", "validator4", "validator5"}
	for _, pubkey := range validators {
		err := stakePool.RegisterValidator(pubkey, big.NewInt(10000000), 5)
		if err != nil {
			t.Fatalf("Failed to register validator %s: %v", pubkey, err)
		}
		
		// Manually set state to active for testing
		if validator, exists := stakePool.validators[pubkey]; exists {
			validator.State = StakeStateActive
		}
	}

	// Test equal distribution
	distribution = stakePool.GetStakeDistribution()
	expectedWeight := 1.0 / float64(len(validators))
	
	if len(distribution) != len(validators) {
		t.Errorf("Distribution count = %d, want %d", len(distribution), len(validators))
	}

	for pubkey, weight := range distribution {
		if weight != expectedWeight {
			t.Errorf("Validator %s weight = %f, want %f", pubkey, weight, expectedWeight)
		}
	}

	// Verify total weight equals 1.0
	totalWeight := 0.0
	for _, weight := range distribution {
		totalWeight += weight
	}
	if totalWeight < 0.99 || totalWeight > 1.01 { // Allow small floating point errors
		t.Errorf("Total weight = %f, want ~1.0", totalWeight)
	}
}

func TestStakePool_DistributeRewards(t *testing.T) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10, 8640)

	// Register validators with different commissions
	validators := []struct {
		pubkey     string
		commission uint8
	}{
		{"validator1", 5},
		{"validator2", 10},
		{"validator3", 15},
	}

	for _, v := range validators {
		err := stakePool.RegisterValidator(v.pubkey, big.NewInt(10000000), v.commission)
		if err != nil {
			t.Fatalf("Failed to register validator %s: %v", v.pubkey, err)
		}
		
		// Set to active and add some delegators
		if validator, exists := stakePool.validators[v.pubkey]; exists {
			validator.State = StakeStateActive
			validator.Delegators[v.pubkey] = big.NewInt(5000000)  // Self-delegation
			validator.Delegators["delegator_"+v.pubkey] = big.NewInt(5000000) // External delegation
		}
	}

	epochRewards := big.NewInt(300000000) // 300M tokens
	slot := uint64(1000)

	rewards := stakePool.DistributeRewards(epochRewards, slot)

	// Each validator should get 100M tokens (300M / 3 validators)
	expectedRewardPerValidator := big.NewInt(100000000)

	for _, v := range validators {
		// Calculate expected commission
		expectedCommission := new(big.Int).Mul(expectedRewardPerValidator, big.NewInt(int64(v.commission)))
		expectedCommission.Div(expectedCommission, big.NewInt(100))

		actualCommission := rewards[v.pubkey]
		if actualCommission == nil {
			t.Errorf("Validator %s received no commission", v.pubkey)
			continue
		}

		if actualCommission.Cmp(expectedCommission) != 0 {
			t.Errorf("Validator %s commission = %v, want %v", v.pubkey, actualCommission, expectedCommission)
		}

		// Check delegator rewards
		delegatorReward := rewards["delegator_"+v.pubkey]
		if delegatorReward == nil {
			t.Errorf("Delegator for %s received no reward", v.pubkey)
			continue
		}

		// Delegator should get half of remaining rewards (since they have 50% of the stake)
		expectedDelegatorReward := new(big.Int).Sub(expectedRewardPerValidator, expectedCommission)
		expectedDelegatorReward.Div(expectedDelegatorReward, big.NewInt(2)) // 50% of remaining

		if delegatorReward.Cmp(expectedDelegatorReward) != 0 {
			t.Errorf("Delegator reward = %v, want %v", delegatorReward, expectedDelegatorReward)
		}
	}
}

func TestStakePool_MaxValidators(t *testing.T) {
	minStake := big.NewInt(1000000)
	maxValidators := 2
	stakePool := NewStakePool(minStake, maxValidators, 8640)

	// Register maximum number of validators
	for i := 0; i < maxValidators; i++ {
		err := stakePool.RegisterValidator(fmt.Sprintf("validator%d", i+1), big.NewInt(10000000), 5)
		if err != nil {
			t.Fatalf("Failed to register validator %d: %v", i+1, err)
		}
	}

	// Try to register one more validator
	err := stakePool.RegisterValidator("validator3", big.NewInt(10000000), 5)
	if err == nil {
		t.Errorf("Expected error when registering validator beyond maximum")
	}
	if err.Error() != "maximum number of validators reached" {
		t.Errorf("Wrong error message: %v", err.Error())
	}
}

func BenchmarkStakePool_RegisterValidator(b *testing.B) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 10000, 8640)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pubkey := fmt.Sprintf("validator%d", i)
		stakePool.RegisterValidator(pubkey, big.NewInt(10000000), 5)
	}
}

func BenchmarkStakePool_GetStakeDistribution(b *testing.B) {
	minStake := big.NewInt(1000000)
	stakePool := NewStakePool(minStake, 1000, 8640)

	// Register 100 validators
	for i := 0; i < 100; i++ {
		pubkey := fmt.Sprintf("validator%d", i)
		stakePool.RegisterValidator(pubkey, big.NewInt(10000000), 5)
		if validator, exists := stakePool.validators[pubkey]; exists {
			validator.State = StakeStateActive
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stakePool.GetStakeDistribution()
	}
}
