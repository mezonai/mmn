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
	stakePool     *StakePool
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

// GenerateRandomizedSchedule generates a randomized but deterministic schedule weighted by stake
func (sws *StakeWeightedScheduler) GenerateRandomizedSchedule(epoch uint64, seed []byte) (*poh.LeaderSchedule, error) {
	activeValidators := sws.stakePool.GetActiveValidators()

	if len(activeValidators) == 0 {
		return nil, errors.New("no active validators")
	}

	// Get actual stake weights instead of equal distribution
	distribution := sws.stakePool.GetStakeDistribution()

	// Generate weighted slot assignments
	entries, err := sws.generateWeightedSlotAssignments(distribution, epoch, seed)
	if err != nil {
		return nil, err
	}

	return poh.NewLeaderSchedule(entries)
}

// generateWeightedSlotAssignments assigns slots based on stake weights with randomization
func (sws *StakeWeightedScheduler) generateWeightedSlotAssignments(distribution map[string]float64, epoch uint64, seed []byte) ([]poh.LeaderScheduleEntry, error) {
	if len(distribution) == 0 {
		return nil, errors.New("no validators in distribution")
	}

	// Create deterministic randomness from epoch and seed
	epochSeed := sha256.Sum256(append(seed, []byte(fmt.Sprintf("epoch_%d", epoch))...))

	// Convert stakes to slot allocations
	totalStake := big.NewFloat(0)
	stakeWeights := make(map[string]*big.Float)

	for validator, weight := range distribution {
		stakeWeight := big.NewFloat(weight)
		stakeWeights[validator] = stakeWeight
		totalStake.Add(totalStake, stakeWeight)
	}

	// Allocate slots proportionally to stake
	var entries []poh.LeaderScheduleEntry
	epochStartSlot := epoch * sws.slotsPerEpoch
	currentSlot := epochStartSlot

	// Generate randomized slot assignments using stake weights
	for i := uint64(0); i < sws.slotsPerEpoch; i++ {
		// Use deterministic randomness to select validator
		slotSeed := sha256.Sum256(append(epochSeed[:], []byte(fmt.Sprintf("slot_%d", i))...))

		// Convert seed to selection probability
		seedBig := new(big.Int).SetBytes(slotSeed[:])
		maxInt := new(big.Int).Exp(big.NewInt(2), big.NewInt(256), nil)
		probability := new(big.Float).Quo(new(big.Float).SetInt(seedBig), new(big.Float).SetInt(maxInt))

		// Select validator based on cumulative stake weights
		selectedValidator := ""
		cumulativeWeight := big.NewFloat(0)
		threshold := new(big.Float).Mul(probability, totalStake)

		for validator, weight := range stakeWeights {
			cumulativeWeight.Add(cumulativeWeight, weight)
			if cumulativeWeight.Cmp(threshold) >= 0 {
				selectedValidator = validator
				break
			}
		}

		// Fallback to first validator if none selected (shouldn't happen)
		if selectedValidator == "" {
			for validator := range stakeWeights {
				selectedValidator = validator
				break
			}
		}

		// Group consecutive slots for same validator
		if len(entries) == 0 || entries[len(entries)-1].Leader != selectedValidator {
			entries = append(entries, poh.LeaderScheduleEntry{
				StartSlot: currentSlot,
				EndSlot:   currentSlot,
				Leader:    selectedValidator,
			})
		} else {
			// Extend the current validator's range
			entries[len(entries)-1].EndSlot = currentSlot
		}

		currentSlot++
	}

	return entries, nil
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
