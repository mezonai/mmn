package staking

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"
)

// StakeState represents the state of a validator's stake
type StakeState int

const (
	StakeStateActive StakeState = iota
	StakeStateActivating
	StakeStateDeactivating
	StakeStateInactive
)

// ValidatorInfo contains information about a validator
type ValidatorInfo struct {
	Pubkey         string              `json:"pubkey"`
	StakeAmount    *big.Int            `json:"stake_amount"`
	State          StakeState          `json:"state"`
	ActivationSlot uint64              `json:"activation_slot"`
	CreatedAt      time.Time           `json:"created_at"`
	LastRewardSlot uint64              `json:"last_reward_slot"`
	Delegators     map[string]*big.Int `json:"delegators"` // delegator pubkey -> amount
}

// StakePool manages all validator stakes and delegations
type StakePool struct {
	mu         sync.RWMutex
	validators map[string]*ValidatorInfo `json:"validators"`
	totalStake *big.Int                  `json:"total_stake"`

	// Configuration
	minStakeAmount  *big.Int `json:"min_stake_amount"`
	maxValidators   int      `json:"max_validators"`
	unstakeCooldown uint64   `json:"unstake_cooldown"` // slots
	activationDelay uint64   `json:"activation_delay"` // slots

	// Epoch management
	currentEpoch  uint64 `json:"current_epoch"`
	slotsPerEpoch uint64 `json:"slots_per_epoch"`
}

// NewStakePool creates a new stake pool
func NewStakePool(minStakeAmount *big.Int, maxValidators int, slotsPerEpoch uint64) *StakePool {
	return &StakePool{
		validators:      make(map[string]*ValidatorInfo),
		totalStake:      big.NewInt(0),
		minStakeAmount:  minStakeAmount,
		maxValidators:   maxValidators,
		unstakeCooldown: 432000, // ~3 days at 400ms slots
		activationDelay: 8640,   // ~1 hour at 400ms slots
		slotsPerEpoch:   slotsPerEpoch,
	}
}

// RegisterValidator registers a new validator
func (sp *StakePool) RegisterValidator(pubkey string, stakeAmount *big.Int) error {
	return sp.registerValidator(pubkey, stakeAmount, false)
}

// RegisterGenesisValidator registers a genesis validator that activates immediately
func (sp *StakePool) RegisterGenesisValidator(pubkey string, stakeAmount *big.Int) error {
	return sp.registerValidator(pubkey, stakeAmount, true)
}

// registerValidator is the internal function for validator registration
func (sp *StakePool) registerValidator(pubkey string, stakeAmount *big.Int, isGenesis bool) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if stakeAmount.Cmp(sp.minStakeAmount) < 0 {
		return fmt.Errorf("stake amount %s is below minimum %s", stakeAmount.String(), sp.minStakeAmount.String())
	}

	if len(sp.validators) >= sp.maxValidators {
		return errors.New("maximum number of validators reached")
	}

	if _, exists := sp.validators[pubkey]; exists {
		return errors.New("validator already registered")
	}

	currentSlot := sp.getCurrentSlot()
	activationSlot := currentSlot + sp.activationDelay
	state := StakeStateActivating

	// Genesis validators activate immediately
	if isGenesis {
		activationSlot = currentSlot
		state = StakeStateActive
	}

	validator := &ValidatorInfo{
		Pubkey:         pubkey,
		StakeAmount:    new(big.Int).Set(stakeAmount),
		State:          state,
		ActivationSlot: activationSlot,
		CreatedAt:      time.Now(),
		Delegators:     make(map[string]*big.Int),
	}

	sp.validators[pubkey] = validator
	sp.totalStake.Add(sp.totalStake, stakeAmount)

	return nil
}

// Delegate stakes tokens to a validator
func (sp *StakePool) Delegate(delegatorPubkey, validatorPubkey string, amount *big.Int) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	validator, exists := sp.validators[validatorPubkey]
	if !exists {
		return errors.New("validator not found")
	}

	if validator.State != StakeStateActive && validator.State != StakeStateActivating {
		return errors.New("cannot delegate to inactive validator")
	}

	if amount.Sign() <= 0 {
		return errors.New("delegation amount must be positive")
	}

	// Add to existing delegation or create new
	if existing, exists := validator.Delegators[delegatorPubkey]; exists {
		existing.Add(existing, amount)
	} else {
		validator.Delegators[delegatorPubkey] = new(big.Int).Set(amount)
	}

	validator.StakeAmount.Add(validator.StakeAmount, amount)
	sp.totalStake.Add(sp.totalStake, amount)

	return nil
}

// Undelegate removes stake from a validator
func (sp *StakePool) Undelegate(delegatorPubkey, validatorPubkey string, amount *big.Int) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	validator, exists := sp.validators[validatorPubkey]
	if !exists {
		return errors.New("validator not found")
	}

	delegation, exists := validator.Delegators[delegatorPubkey]
	if !exists {
		return errors.New("no delegation found")
	}

	if delegation.Cmp(amount) < 0 {
		return errors.New("insufficient delegation amount")
	}

	delegation.Sub(delegation, amount)
	if delegation.Sign() == 0 {
		delete(validator.Delegators, delegatorPubkey)
	}

	validator.StakeAmount.Sub(validator.StakeAmount, amount)
	sp.totalStake.Sub(sp.totalStake, amount)

	// Deactivate validator if stake falls below minimum
	if validator.StakeAmount.Cmp(sp.minStakeAmount) < 0 {
		validator.State = StakeStateDeactivating
	}

	return nil
}

// GetActiveValidators returns all active validators
func (sp *StakePool) GetActiveValidators() map[string]*ValidatorInfo {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	active := make(map[string]*ValidatorInfo)
	currentSlot := sp.getCurrentSlot()

	for pubkey, validator := range sp.validators {
		if validator.State == StakeStateActive ||
			(validator.State == StakeStateActivating && currentSlot >= validator.ActivationSlot) {
			// Update state if activation slot reached
			if validator.State == StakeStateActivating && currentSlot >= validator.ActivationSlot {
				validator.State = StakeStateActive
			}
			active[pubkey] = validator
		}
	}

	return active
}

// GetValidatorStakeWeight returns the stake weight (0-1) for a validator
func (sp *StakePool) GetValidatorStakeWeight(pubkey string) float64 {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	validator, exists := sp.validators[pubkey]
	if !exists || sp.totalStake.Sign() == 0 {
		return 0.0
	}

	// Calculate weight as stake_amount / total_stake
	weight := new(big.Float).SetInt(validator.StakeAmount)
	total := new(big.Float).SetInt(sp.totalStake)
	result, _ := new(big.Float).Quo(weight, total).Float64()

	return result
}

// GetStakeDistribution returns stake distribution among active validators
func (sp *StakePool) GetStakeDistribution() map[string]float64 {
	activeValidators := sp.GetActiveValidators()
	distribution := make(map[string]float64)

	if len(activeValidators) == 0 {
		return distribution
	}

	// Equal distribution: 1/n for each active validator
	weight := 1.0 / float64(len(activeValidators))
	for pubkey := range activeValidators {
		distribution[pubkey] = weight
	}

	return distribution
}

// UpdateEpoch updates the current epoch
func (sp *StakePool) UpdateEpoch(newEpoch uint64) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.currentEpoch = newEpoch
}

// DistributeRewards distributes epoch rewards to validators and delegators
func (sp *StakePool) DistributeRewards(epochRewards *big.Int, slot uint64) map[string]*big.Int {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	rewards := make(map[string]*big.Int)
	activeValidators := sp.getActiveValidatorsUnsafe()

	if len(activeValidators) == 0 || epochRewards.Sign() <= 0 {
		return rewards
	}

	// Equal distribution among active validators
	rewardPerValidator := new(big.Int).Div(epochRewards, big.NewInt(int64(len(activeValidators))))

	for pubkey, validator := range activeValidators {
		if validator.LastRewardSlot >= slot {
			continue // Already rewarded this slot
		}

		validator.LastRewardSlot = slot

		// No commission - all rewards go to validator directly
		rewards[pubkey] = rewardPerValidator

		// Distribute proportionally to delegators (if any)
		for delegatorPubkey, delegation := range validator.Delegators {
			if delegation.Sign() <= 0 {
				continue
			}

			// Without commission, rewards distributed proportionally based on stake
			// delegation_reward = total_reward * (delegation / total_validator_stake)
			delegatorReward := new(big.Int).Mul(rewardPerValidator, delegation)
			delegatorReward.Div(delegatorReward, validator.StakeAmount)

			if existing, exists := rewards[delegatorPubkey]; exists {
				existing.Add(existing, delegatorReward)
			} else {
				rewards[delegatorPubkey] = delegatorReward
			}
		}
	}

	return rewards
}

// GetValidatorInfo returns information about a specific validator
func (sp *StakePool) GetValidatorInfo(pubkey string) (*ValidatorInfo, bool) {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	validator, exists := sp.validators[pubkey]
	if !exists {
		return nil, false
	}

	// Return a copy to prevent external mutation
	info := &ValidatorInfo{
		Pubkey:         validator.Pubkey,
		StakeAmount:    new(big.Int).Set(validator.StakeAmount),
		State:          validator.State,
		ActivationSlot: validator.ActivationSlot,
		CreatedAt:      validator.CreatedAt,
		LastRewardSlot: validator.LastRewardSlot,
		Delegators:     make(map[string]*big.Int),
	}

	for delPubkey, amount := range validator.Delegators {
		info.Delegators[delPubkey] = new(big.Int).Set(amount)
	}

	return info, true
}

// GetTotalStake returns the total stake in the pool
func (sp *StakePool) GetTotalStake() *big.Int {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return new(big.Int).Set(sp.totalStake)
}

// Private helper methods

func (sp *StakePool) getCurrentSlot() uint64 {
	// This should be implemented to return current blockchain slot
	// For now, return a placeholder
	return uint64(time.Now().Unix() / 400) // Assuming 400ms slots
}

func (sp *StakePool) getActiveValidatorsUnsafe() map[string]*ValidatorInfo {
	active := make(map[string]*ValidatorInfo)
	currentSlot := sp.getCurrentSlot()

	for pubkey, validator := range sp.validators {
		if validator.State == StakeStateActive ||
			(validator.State == StakeStateActivating && currentSlot >= validator.ActivationSlot) {
			active[pubkey] = validator
		}
	}

	return active
}

// GenerateStakeProof generates a cryptographic proof of stake for a validator
func (sp *StakePool) GenerateStakeProof(validatorPubkey string, slot uint64, privKey ed25519.PrivateKey) ([]byte, error) {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	validator, exists := sp.validators[validatorPubkey]
	if !exists {
		return nil, errors.New("validator not found")
	}

	if validator.State != StakeStateActive {
		return nil, errors.New("validator not active")
	}

	// Create proof data
	proofData := fmt.Sprintf("%s:%d:%s", validatorPubkey, slot, validator.StakeAmount.String())
	hash := sha256.Sum256([]byte(proofData))

	// Sign the hash
	signature := ed25519.Sign(privKey, hash[:])

	return signature, nil
}

// VerifyStakeProof verifies a stake proof
func (sp *StakePool) VerifyStakeProof(validatorPubkey string, slot uint64, proof []byte, pubKey ed25519.PublicKey) bool {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	validator, exists := sp.validators[validatorPubkey]
	if !exists || validator.State != StakeStateActive {
		return false
	}

	// Recreate proof data
	proofData := fmt.Sprintf("%s:%d:%s", validatorPubkey, slot, validator.StakeAmount.String())
	hash := sha256.Sum256([]byte(proofData))

	// Verify signature
	return ed25519.Verify(pubKey, hash[:], proof)
}
