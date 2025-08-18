package staking

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/mezonai/mmn/poh"
)

// StakeWeightedScheduler creates leader schedules based on stake weights
type StakeWeightedScheduler struct {
	stakePool   *StakePool
	slotsPerEpoch uint64
}

// NewStakeWeightedScheduler creates a new stake-weighted scheduler
func NewStakeWeightedScheduler(stakePool *StakePool, slotsPerEpoch uint64) *StakeWeightedScheduler {
	return &StakeWeightedScheduler{
		stakePool:     stakePool,
		slotsPerEpoch: slotsPerEpoch,
	}
}

// GenerateLeaderSchedule generates a leader schedule for an epoch based on stake weights
func (sws *StakeWeightedScheduler) GenerateLeaderSchedule(epoch uint64, seed []byte) (*poh.LeaderSchedule, error) {
	activeValidators := sws.stakePool.GetActiveValidators()
	
	if len(activeValidators) == 0 {
		return nil, errors.New("no active validators")
	}
	
	// Get stake distribution (equal distribution as per requirements)
	distribution := sws.stakePool.GetStakeDistribution()
	
	// Generate slot assignments
	entries, err := sws.generateSlotAssignments(distribution, epoch, seed)
	if err != nil {
		return nil, err
	}
	
	return poh.NewLeaderSchedule(entries)
}

// generateSlotAssignments assigns slots to validators based on their stake weights
func (sws *StakeWeightedScheduler) generateSlotAssignments(distribution map[string]float64, epoch uint64, seed []byte) ([]poh.LeaderScheduleEntry, error) {
	if len(distribution) == 0 {
		return nil, errors.New("no validators in distribution")
	}
	
	// Create ordered list of validators for deterministic assignment
	validators := make([]string, 0, len(distribution))
	for pubkey := range distribution {
		validators = append(validators, pubkey)
	}
	sort.Strings(validators) // Deterministic ordering
	
	var entries []poh.LeaderScheduleEntry
	slotsAssigned := uint64(0)
	
	// Equal distribution: each validator gets slots_per_epoch / num_validators slots
	slotsPerValidator := sws.slotsPerEpoch / uint64(len(validators))
	remainingSlots := sws.slotsPerEpoch % uint64(len(validators))
	
	epochStartSlot := epoch * sws.slotsPerEpoch
	
	for i, validator := range validators {
		slotCount := slotsPerValidator
		
		// Distribute remaining slots to first few validators
		if uint64(i) < remainingSlots {
			slotCount++
		}
		
		if slotCount > 0 {
			startSlot := epochStartSlot + slotsAssigned
			endSlot := startSlot + slotCount - 1
			
			entries = append(entries, poh.LeaderScheduleEntry{
				StartSlot: startSlot,
				EndSlot:   endSlot,
				Leader:    validator,
			})
			
			slotsAssigned += slotCount
		}
	}
	
	return entries, nil
}

// GenerateRandomizedSchedule generates a randomized but deterministic schedule
func (sws *StakeWeightedScheduler) GenerateRandomizedSchedule(epoch uint64, seed []byte) (*poh.LeaderSchedule, error) {
	activeValidators := sws.stakePool.GetActiveValidators()
	
	if len(activeValidators) == 0 {
		return nil, errors.New("no active validators")
	}
	
	// Create validator list
	validators := make([]string, 0, len(activeValidators))
	for pubkey := range activeValidators {
		validators = append(validators, pubkey)
	}
	
	// Shuffle validators deterministically using epoch and seed
	shuffledValidators := sws.shuffleValidators(validators, epoch, seed)
	
	// Assign slots in round-robin fashion
	var entries []poh.LeaderScheduleEntry
	epochStartSlot := epoch * sws.slotsPerEpoch
	
	for slot := uint64(0); slot < sws.slotsPerEpoch; slot++ {
		validatorIndex := slot % uint64(len(shuffledValidators))
		validator := shuffledValidators[validatorIndex]
		
		// Create single-slot entries for maximum randomization
		entries = append(entries, poh.LeaderScheduleEntry{
			StartSlot: epochStartSlot + slot,
			EndSlot:   epochStartSlot + slot,
			Leader:    validator,
		})
	}
	
	return poh.NewLeaderSchedule(entries)
}

// shuffleValidators shuffles validators deterministically using epoch and seed
func (sws *StakeWeightedScheduler) shuffleValidators(validators []string, epoch uint64, seed []byte) []string {
	// Create deterministic random source from epoch and seed
	epochBytes := make([]byte, 8)
	for i := 0; i < 8; i++ {
		epochBytes[i] = byte(epoch >> (i * 8))
	}
	
	combinedSeed := append(seed, epochBytes...)
	hash := sha256.Sum256(combinedSeed)
	
	// Fisher-Yates shuffle with deterministic random
	shuffled := make([]string, len(validators))
	copy(shuffled, validators)
	
	for i := len(shuffled) - 1; i > 0; i-- {
		// Generate deterministic random index
		hashInput := append(hash[:], byte(i))
		randHash := sha256.Sum256(hashInput)
		
		// Convert hash to index
		randBig := new(big.Int).SetBytes(randHash[:])
		j := new(big.Int).Mod(randBig, big.NewInt(int64(i+1))).Int64()
		
		// Swap
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	
	return shuffled
}

// GetLeaderForSlot returns the leader for a specific slot
func (sws *StakeWeightedScheduler) GetLeaderForSlot(schedule *poh.LeaderSchedule, slot uint64) (string, bool) {
	return schedule.LeaderAt(slot)
}

// ValidateLeaderSchedule validates that a leader schedule is correct for the given epoch
func (sws *StakeWeightedScheduler) ValidateLeaderSchedule(schedule *poh.LeaderSchedule, epoch uint64, seed []byte) error {
	// Generate expected schedule
	expectedSchedule, err := sws.GenerateLeaderSchedule(epoch, seed)
	if err != nil {
		return fmt.Errorf("failed to generate expected schedule: %w", err)
	}
	
	// Compare schedules
	epochStartSlot := epoch * sws.slotsPerEpoch
	epochEndSlot := epochStartSlot + sws.slotsPerEpoch - 1
	
	for slot := epochStartSlot; slot <= epochEndSlot; slot++ {
		expectedLeader, expectedExists := expectedSchedule.LeaderAt(slot)
		actualLeader, actualExists := schedule.LeaderAt(slot)
		
		if expectedExists != actualExists {
			return fmt.Errorf("leader existence mismatch at slot %d", slot)
		}
		
		if expectedExists && expectedLeader != actualLeader {
			return fmt.Errorf("leader mismatch at slot %d: expected %s, got %s", 
				slot, expectedLeader, actualLeader)
		}
	}
	
	return nil
}

// GetEpochFromSlot returns the epoch number for a given slot
func (sws *StakeWeightedScheduler) GetEpochFromSlot(slot uint64) uint64 {
	return slot / sws.slotsPerEpoch
}

// GetEpochStartSlot returns the first slot of an epoch
func (sws *StakeWeightedScheduler) GetEpochStartSlot(epoch uint64) uint64 {
	return epoch * sws.slotsPerEpoch
}

// GetEpochEndSlot returns the last slot of an epoch
func (sws *StakeWeightedScheduler) GetEpochEndSlot(epoch uint64) uint64 {
	return (epoch+1)*sws.slotsPerEpoch - 1
}

// GetSlotsPerEpoch returns the number of slots per epoch
func (sws *StakeWeightedScheduler) GetSlotsPerEpoch() uint64 {
	return sws.slotsPerEpoch
}

// CalculateStakeRewards calculates rewards for an epoch based on performance
func (sws *StakeWeightedScheduler) CalculateStakeRewards(epoch uint64, totalRewards *big.Int, performanceMetrics map[string]float64) map[string]*big.Int {
	rewards := make(map[string]*big.Int)
	activeValidators := sws.stakePool.GetActiveValidators()
	
	if len(activeValidators) == 0 || totalRewards.Sign() <= 0 {
		return rewards
	}
	
	// Base reward per validator (equal distribution)
	baseReward := new(big.Int).Div(totalRewards, big.NewInt(int64(len(activeValidators))))
	
	for pubkey := range activeValidators {
		validatorReward := new(big.Int).Set(baseReward)
		
		// Apply performance multiplier if available
		if performance, exists := performanceMetrics[pubkey]; exists {
			// Performance ranges from 0.0 to 1.0
			// Multiply reward by performance
			performanceInt := big.NewInt(int64(performance * 1000)) // Scale to avoid floating point
			validatorReward.Mul(validatorReward, performanceInt)
			validatorReward.Div(validatorReward, big.NewInt(1000))
		}
		
		rewards[pubkey] = validatorReward
	}
	
	return rewards
}
