package poh

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"sort"
	"sync"
)

// Validator represents a validator with stake information
type Validator struct {
	PubKey          string `json:"pubkey"`
	Stake           uint64 `json:"stake"`            // Amount of stake
	ActiveStake     uint64 `json:"active_stake"`     // Active stake for current epoch
	VoteAccount     string `json:"vote_account"`     // Vote account address
	LastVoteSlot    uint64 `json:"last_vote_slot"`   // Last slot this validator voted
	Commission      uint64 `json:"commission"`       // Commission rate (0-100, but we won't use rewards)
	IsActive        bool   `json:"is_active"`        // Whether validator is active
	ActivatedSlot   uint64 `json:"activated_slot"`   // Slot when validator became active
	DeactivatedSlot uint64 `json:"deactivated_slot"` // Slot when validator was deactivated
}

// LeaderSlotRange represents a continuous slot range assigned to a leader
type LeaderSlotRange struct {
	Leader    string `json:"leader"`     // Validator pubkey
	StartSlot uint64 `json:"start_slot"` // First slot (inclusive)
	EndSlot   uint64 `json:"end_slot"`   // Last slot (inclusive)
	SlotCount uint64 `json:"slot_count"` // Number of consecutive slots
}

// EpochInfo contains information about an epoch
type EpochInfo struct {
	Epoch            uint64                `json:"epoch"`
	StartSlot        uint64                `json:"start_slot"`
	EndSlot          uint64                `json:"end_slot"`
	SlotsInEpoch     uint64                `json:"slots_in_epoch"`
	AbsoluteSlot     uint64                `json:"absolute_slot"`
	BlockHeight      uint64                `json:"block_height"`
	TransactionCount uint64                `json:"transaction_count"`
	Validators       map[string]*Validator `json:"validators"`
	TotalStake       uint64                `json:"total_stake"`
	LeaderSchedule   []string              `json:"leader_schedule"` // Ordered list of validator pubkeys
	LeaderRanges     []LeaderSlotRange     `json:"leader_ranges"`   // Leader assignments with slot ranges
}

// DynamicLeaderSchedule manages epoch-based leader rotation using PoS
type DynamicLeaderSchedule struct {
	mu                  sync.RWMutex
	currentEpoch        *EpochInfo
	nextEpoch           *EpochInfo
	validators          map[string]*Validator
	slotsPerEpoch       uint64
	epochStartSlot      uint64
	seed                []byte
	lastScheduleSlot    uint64
	maxConsecutiveSlots uint64 // Maximum consecutive slots per leader (default: 4)
}

// NewDynamicLeaderSchedule creates a new dynamic leader scheduler
func NewDynamicLeaderSchedule(slotsPerEpoch uint64, genesisValidators map[string]*Validator) *DynamicLeaderSchedule {
	seed := make([]byte, 32)
	rand.Read(seed)

	dls := &DynamicLeaderSchedule{
		validators:          make(map[string]*Validator),
		slotsPerEpoch:       slotsPerEpoch,
		epochStartSlot:      0,
		seed:                seed,
		lastScheduleSlot:    0,
		maxConsecutiveSlots: 4, // Maximum 4 consecutive slots per leader
	}

	// Initialize with genesis validators
	for pubkey, validator := range genesisValidators {
		dls.validators[pubkey] = &Validator{
			PubKey:        validator.PubKey,
			Stake:         validator.Stake,
			ActiveStake:   validator.Stake,
			VoteAccount:   validator.VoteAccount,
			IsActive:      true,
			ActivatedSlot: 0,
		}
	}

	// Generate initial epoch
	dls.currentEpoch = dls.generateEpoch(0, 0)
	dls.nextEpoch = dls.generateEpoch(1, dls.slotsPerEpoch)

	return dls
}

// GetCurrentEpoch returns the current epoch information
func (dls *DynamicLeaderSchedule) GetCurrentEpoch(currentSlot uint64) *EpochInfo {
	dls.mu.RLock()
	defer dls.mu.RUnlock()

	// Check if we need to advance to next epoch
	if currentSlot >= dls.currentEpoch.EndSlot && dls.nextEpoch != nil {
		dls.mu.RUnlock()
		dls.mu.Lock()

		// Double check after acquiring write lock
		if currentSlot >= dls.currentEpoch.EndSlot && dls.nextEpoch != nil {
			dls.advanceToNextEpoch(currentSlot)
		}

		dls.mu.Unlock()
		dls.mu.RLock()
	}

	return dls.currentEpoch
}

// LeaderAt returns the leader for a given slot using PoS-based schedule
func (dls *DynamicLeaderSchedule) LeaderAt(slot uint64) (string, bool) {
	epoch := dls.GetCurrentEpoch(slot)
	if epoch == nil || len(epoch.LeaderSchedule) == 0 {
		return "", false
	}

	// Calculate position within epoch
	slotInEpoch := slot - epoch.StartSlot
	if slotInEpoch >= uint64(len(epoch.LeaderSchedule)) {
		return "", false
	}

	return epoch.LeaderSchedule[slotInEpoch], true
}

// GetLeaderRange returns the leader slot range containing the given slot
func (dls *DynamicLeaderSchedule) GetLeaderRange(slot uint64) (*LeaderSlotRange, bool) {
	epoch := dls.GetCurrentEpoch(slot)
	if epoch == nil || len(epoch.LeaderRanges) == 0 {
		return nil, false
	}

	// Find the range containing this slot
	for _, r := range epoch.LeaderRanges {
		if slot >= epoch.StartSlot+r.StartSlot && slot <= epoch.StartSlot+r.EndSlot {
			// Adjust range to absolute slot numbers
			adjustedRange := LeaderSlotRange{
				Leader:    r.Leader,
				StartSlot: epoch.StartSlot + r.StartSlot,
				EndSlot:   epoch.StartSlot + r.EndSlot,
				SlotCount: r.SlotCount,
			}
			return &adjustedRange, true
		}
	}

	return nil, false
}

// GetCurrentEpochRanges returns all leader ranges for the current epoch
func (dls *DynamicLeaderSchedule) GetCurrentEpochRanges(currentSlot uint64) []LeaderSlotRange {
	epoch := dls.GetCurrentEpoch(currentSlot)
	if epoch == nil {
		return []LeaderSlotRange{}
	}

	// Adjust ranges to absolute slot numbers
	ranges := make([]LeaderSlotRange, len(epoch.LeaderRanges))
	for i, r := range epoch.LeaderRanges {
		ranges[i] = LeaderSlotRange{
			Leader:    r.Leader,
			StartSlot: epoch.StartSlot + r.StartSlot,
			EndSlot:   epoch.StartSlot + r.EndSlot,
			SlotCount: r.SlotCount,
		}
	}

	return ranges
}

// SetMaxConsecutiveSlots sets the maximum consecutive slots per leader
func (dls *DynamicLeaderSchedule) SetMaxConsecutiveSlots(maxSlots uint64) {
	dls.mu.Lock()
	defer dls.mu.Unlock()
	dls.maxConsecutiveSlots = maxSlots
}

// GetMaxConsecutiveSlots returns the maximum consecutive slots per leader
func (dls *DynamicLeaderSchedule) GetMaxConsecutiveSlots() uint64 {
	dls.mu.RLock()
	defer dls.mu.RUnlock()
	return dls.maxConsecutiveSlots
}

// AddValidator adds a new validator to the system
func (dls *DynamicLeaderSchedule) AddValidator(validator *Validator) {
	dls.mu.Lock()
	defer dls.mu.Unlock()

	dls.validators[validator.PubKey] = validator
}

// UpdateValidatorStake updates a validator's stake
func (dls *DynamicLeaderSchedule) UpdateValidatorStake(pubkey string, newStake uint64) {
	dls.mu.Lock()
	defer dls.mu.Unlock()

	if validator, exists := dls.validators[pubkey]; exists {
		validator.Stake = newStake
		// Active stake will be updated in next epoch
	}
}

// ActivateValidator activates a validator
func (dls *DynamicLeaderSchedule) ActivateValidator(pubkey string, slot uint64) {
	dls.mu.Lock()
	defer dls.mu.Unlock()

	if validator, exists := dls.validators[pubkey]; exists {
		validator.IsActive = true
		validator.ActivatedSlot = slot
		validator.ActiveStake = validator.Stake
	}
}

// DeactivateValidator deactivates a validator
func (dls *DynamicLeaderSchedule) DeactivateValidator(pubkey string, slot uint64) {
	dls.mu.Lock()
	defer dls.mu.Unlock()

	if validator, exists := dls.validators[pubkey]; exists {
		validator.IsActive = false
		validator.DeactivatedSlot = slot
		validator.ActiveStake = 0
	}
}

// RecordVote records a vote from a validator
func (dls *DynamicLeaderSchedule) RecordVote(validatorPubkey string, slot uint64) {
	dls.mu.Lock()
	defer dls.mu.Unlock()

	if validator, exists := dls.validators[validatorPubkey]; exists {
		validator.LastVoteSlot = slot
	}
}

// generateEpoch creates a new epoch with PoS-based leader schedule
func (dls *DynamicLeaderSchedule) generateEpoch(epochNum uint64, startSlot uint64) *EpochInfo {
	activeValidators := make([]*Validator, 0)
	totalStake := uint64(0)

	// Collect active validators and calculate total stake
	for _, validator := range dls.validators {
		if validator.IsActive && validator.ActiveStake > 0 {
			activeValidators = append(activeValidators, validator)
			totalStake += validator.ActiveStake
		}
	}

	if len(activeValidators) == 0 {
		return nil
	}

	// Sort validators by pubkey for deterministic ordering
	sort.Slice(activeValidators, func(i, j int) bool {
		return activeValidators[i].PubKey < activeValidators[j].PubKey
	})

	// Generate epoch seed using previous epoch hash
	epochSeed := dls.generateEpochSeed(epochNum)

	// Create weighted leader schedule with ranges
	leaderSchedule, leaderRanges := dls.generateLeaderSchedule(activeValidators, totalStake, epochSeed)

	// Create validator map for this epoch
	validators := make(map[string]*Validator)
	for _, validator := range activeValidators {
		validators[validator.PubKey] = &Validator{
			PubKey:          validator.PubKey,
			Stake:           validator.Stake,
			ActiveStake:     validator.ActiveStake,
			VoteAccount:     validator.VoteAccount,
			LastVoteSlot:    validator.LastVoteSlot,
			IsActive:        validator.IsActive,
			ActivatedSlot:   validator.ActivatedSlot,
			DeactivatedSlot: validator.DeactivatedSlot,
		}
	}

	return &EpochInfo{
		Epoch:          epochNum,
		StartSlot:      startSlot,
		EndSlot:        startSlot + dls.slotsPerEpoch - 1,
		SlotsInEpoch:   dls.slotsPerEpoch,
		AbsoluteSlot:   startSlot,
		Validators:     validators,
		TotalStake:     totalStake,
		LeaderSchedule: leaderSchedule,
		LeaderRanges:   leaderRanges,
	}
}

// generateEpochSeed creates a deterministic seed for epoch scheduling
func (dls *DynamicLeaderSchedule) generateEpochSeed(epochNum uint64) []byte {
	hasher := sha256.New()
	hasher.Write(dls.seed)

	epochBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(epochBytes, epochNum)
	hasher.Write(epochBytes)

	return hasher.Sum(nil)
}

// generateLeaderSchedule creates a weighted random leader schedule with consecutive slot constraints
func (dls *DynamicLeaderSchedule) generateLeaderSchedule(validators []*Validator, totalStake uint64, seed []byte) ([]string, []LeaderSlotRange) {
	if len(validators) == 0 || totalStake == 0 {
		return []string{}, []LeaderSlotRange{}
	}

	schedule := make([]string, dls.slotsPerEpoch)
	ranges := make([]LeaderSlotRange, 0)

	// Create cumulative stake array for weighted selection
	cumulativeStakes := make([]uint64, len(validators))
	cumulativeStakes[0] = validators[0].ActiveStake
	for i := 1; i < len(validators); i++ {
		cumulativeStakes[i] = cumulativeStakes[i-1] + validators[i].ActiveStake
	}

	// Track consecutive slot count for current leader
	lastLeader := ""
	currentRangeStart := uint64(0)
	consecutiveCount := uint64(0)

	// Generate schedule for each slot with consecutive slot constraint
	for slot := uint64(0); slot < dls.slotsPerEpoch; slot++ {
		var selectedValidator string

		// If previous leader has reached max consecutive slots, force different validator
		if lastLeader != "" && consecutiveCount >= dls.maxConsecutiveSlots {
			// Must select a different validator
			maxAttempts := 100
			attempts := 0

			for attempts < maxAttempts {
				// Create slot-specific seed
				slotSeed := sha256.Sum256(append(seed, byte(slot), byte(attempts)))

				// Convert seed to number in range [0, totalStake)
				seedBig := new(big.Int).SetBytes(slotSeed[:])
				totalStakeBig := new(big.Int).SetUint64(totalStake)
				randomStake := new(big.Int).Mod(seedBig, totalStakeBig).Uint64()

				// Find validator with binary search
				validatorIdx := sort.Search(len(cumulativeStakes), func(i int) bool {
					return cumulativeStakes[i] > randomStake
				})

				if validatorIdx < len(validators) {
					candidate := validators[validatorIdx].PubKey

					// Must be different from last leader
					if candidate != lastLeader {
						selectedValidator = candidate
						break
					}
				}

				attempts++
			}

			// Fallback: pick first different validator
			if selectedValidator == "" {
				for _, validator := range validators {
					if validator.PubKey != lastLeader {
						selectedValidator = validator.PubKey
						break
					}
				}
			}
		} else {
			// Normal selection with weighted probability
			maxAttempts := 100
			attempts := 0

			for attempts < maxAttempts {
				// Create slot-specific seed
				slotSeed := sha256.Sum256(append(seed, byte(slot), byte(attempts)))

				// Convert seed to number in range [0, totalStake)
				seedBig := new(big.Int).SetBytes(slotSeed[:])
				totalStakeBig := new(big.Int).SetUint64(totalStake)
				randomStake := new(big.Int).Mod(seedBig, totalStakeBig).Uint64()

				// Find validator with binary search
				validatorIdx := sort.Search(len(cumulativeStakes), func(i int) bool {
					return cumulativeStakes[i] > randomStake
				})

				if validatorIdx < len(validators) {
					selectedValidator = validators[validatorIdx].PubKey
					break
				}

				attempts++
			}
		}

		// Ensure we always have a leader for each slot
		if selectedValidator == "" && len(validators) > 0 {
			selectedValidator = validators[0].PubKey
		}

		schedule[slot] = selectedValidator

		// Update consecutive counting and ranges
		if selectedValidator != lastLeader {
			// Close previous range if exists
			if lastLeader != "" {
				ranges = append(ranges, LeaderSlotRange{
					Leader:    lastLeader,
					StartSlot: currentRangeStart,
					EndSlot:   slot - 1,
					SlotCount: slot - currentRangeStart,
				})
			}

			// Start new range
			currentRangeStart = slot
			lastLeader = selectedValidator
			consecutiveCount = 1
		} else {
			// Same leader, increment count
			consecutiveCount++
		}
	}

	// Close final range
	if lastLeader != "" {
		ranges = append(ranges, LeaderSlotRange{
			Leader:    lastLeader,
			StartSlot: currentRangeStart,
			EndSlot:   dls.slotsPerEpoch - 1,
			SlotCount: dls.slotsPerEpoch - currentRangeStart,
		})
	}

	return schedule, ranges
} // advanceToNextEpoch moves to the next epoch and generates the following one
func (dls *DynamicLeaderSchedule) advanceToNextEpoch(currentSlot uint64) {
	if dls.nextEpoch == nil {
		return
	}

	fmt.Printf("[SCHEDULER] Advancing to epoch %d at slot %d\n", dls.nextEpoch.Epoch, currentSlot)

	// Update validator active stakes based on current stakes
	for _, validator := range dls.validators {
		if validator.IsActive {
			validator.ActiveStake = validator.Stake
		}
	}

	// Move to next epoch
	dls.currentEpoch = dls.nextEpoch

	// Generate new next epoch
	nextEpochNum := dls.currentEpoch.Epoch + 1
	nextStartSlot := dls.currentEpoch.EndSlot + 1
	dls.nextEpoch = dls.generateEpoch(nextEpochNum, nextStartSlot)

	fmt.Printf("[SCHEDULER] Generated epoch %d (slots %d-%d) with %d validators\n",
		dls.nextEpoch.Epoch, dls.nextEpoch.StartSlot, dls.nextEpoch.EndSlot,
		len(dls.nextEpoch.Validators))
}

// GetValidators returns current validators
func (dls *DynamicLeaderSchedule) GetValidators() map[string]*Validator {
	dls.mu.RLock()
	defer dls.mu.RUnlock()

	result := make(map[string]*Validator)
	for pubkey, validator := range dls.validators {
		result[pubkey] = &Validator{
			PubKey:          validator.PubKey,
			Stake:           validator.Stake,
			ActiveStake:     validator.ActiveStake,
			VoteAccount:     validator.VoteAccount,
			LastVoteSlot:    validator.LastVoteSlot,
			IsActive:        validator.IsActive,
			ActivatedSlot:   validator.ActivatedSlot,
			DeactivatedSlot: validator.DeactivatedSlot,
		}
	}
	return result
}

// GetEpochSchedule returns the schedule for current and next epochs
func (dls *DynamicLeaderSchedule) GetEpochSchedule(currentSlot uint64) (*EpochInfo, *EpochInfo) {
	current := dls.GetCurrentEpoch(currentSlot)
	dls.mu.RLock()
	next := dls.nextEpoch
	dls.mu.RUnlock()
	return current, next
}
