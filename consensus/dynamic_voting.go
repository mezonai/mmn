package consensus

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// VoteState represents the state of a vote
type VoteState int

const (
	VoteStatePending VoteState = iota
	VoteStateValid
	VoteStateInvalid
	VoteStateExpired
)

// VoteAccount represents a validator's vote account
type VoteAccount struct {
	VoteAddress     string        `json:"vote_address"`
	ValidatorKey    string        `json:"validator_key"`
	AuthorizedVoter string        `json:"authorized_voter"`
	LastVoteSlot    uint64        `json:"last_vote_slot"`
	LastVoteHash    [32]byte      `json:"last_vote_hash"`
	RootSlot        uint64        `json:"root_slot"`     // Highest slot that has been rooted
	Credits         uint64        `json:"credits"`       // Vote credits earned
	EpochCredits    []EpochCredit `json:"epoch_credits"` // Credits per epoch
	CreatedAt       time.Time     `json:"created_at"`
}

// EpochCredit tracks vote credits for a specific epoch
type EpochCredit struct {
	Epoch           uint64 `json:"epoch"`
	Credits         uint64 `json:"credits"`
	PreviousCredits uint64 `json:"previous_credits"`
}

// VoteLandedSlots tracks which slots a vote was actually included in blocks
type VoteLandedSlots struct {
	Slot      uint64    `json:"slot"`
	Timestamp time.Time `json:"timestamp"`
	Confirmed bool      `json:"confirmed"`
	Finalized bool      `json:"finalized"`
}

// DynamicVote extends the basic Vote with additional PoS features
type DynamicVote struct {
	*Vote                            // Embed basic vote
	VoteAccount    string            `json:"vote_account"`    // Vote account address
	ValidatorStake uint64            `json:"validator_stake"` // Stake amount at time of vote
	SlotRange      []uint64          `json:"slot_range"`      // Range of slots being voted on
	RootSlot       uint64            `json:"root_slot"`       // Root slot being confirmed
	Credits        uint64            `json:"credits"`         // Vote credits to be awarded
	LandedSlots    []VoteLandedSlots `json:"landed_slots"`    // Slots where vote was included
	State          VoteState         `json:"state"`
	CreatedAt      time.Time         `json:"created_at"`
	ProcessedAt    *time.Time        `json:"processed_at"`
}

// DynamicCollector manages dynamic voting with PoS features
type DynamicCollector struct {
	mu              sync.RWMutex
	votes           map[uint64]map[string]*DynamicVote // slot → voterID → DynamicVote
	voteAccounts    map[string]*VoteAccount            // voteAddress → VoteAccount
	validatorStakes map[string]uint64                  // validatorKey → stake amount
	totalStake      uint64                             // Total active stake
	threshold       float64                            // Voting threshold (2/3 for supermajority)
	epochInfo       *EpochInfo                         // Current epoch information
	voteTimeout     time.Duration                      // Vote expiration timeout
	maxSlotRange    uint64                             // Maximum slot range for a single vote
}

// EpochInfo contains epoch-specific voting information
type EpochInfo struct {
	Epoch        uint64            `json:"epoch"`
	StartSlot    uint64            `json:"start_slot"`
	EndSlot      uint64            `json:"end_slot"`
	TotalStake   uint64            `json:"total_stake"`
	ActiveStake  uint64            `json:"active_stake"`
	StakeHistory map[string]uint64 `json:"stake_history"` // Historical stake amounts
}

// NewDynamicCollector creates a new dynamic voting collector
func NewDynamicCollector(validatorStakes map[string]uint64, threshold float64) *DynamicCollector {
	if threshold <= 0 || threshold > 1 {
		threshold = 0.67 // Default to 2/3 supermajority
	}

	totalStake := uint64(0)
	for _, stake := range validatorStakes {
		totalStake += stake
	}

	return &DynamicCollector{
		votes:           make(map[uint64]map[string]*DynamicVote),
		voteAccounts:    make(map[string]*VoteAccount),
		validatorStakes: make(map[string]uint64),
		totalStake:      totalStake,
		threshold:       threshold,
		voteTimeout:     30 * time.Second,
		maxSlotRange:    32, // Maximum 32 slots per vote
	}
}

// UpdateValidatorStake updates a validator's stake amount
func (dc *DynamicCollector) UpdateValidatorStake(validatorKey string, newStake uint64) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	oldStake, exists := dc.validatorStakes[validatorKey]
	dc.validatorStakes[validatorKey] = newStake

	// Update total stake
	if exists {
		dc.totalStake = dc.totalStake - oldStake + newStake
	} else {
		dc.totalStake += newStake
	}
}

// CreateVoteAccount creates a new vote account for a validator
func (dc *DynamicCollector) CreateVoteAccount(voteAddress, validatorKey, authorizedVoter string) error {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if _, exists := dc.voteAccounts[voteAddress]; exists {
		return fmt.Errorf("vote account %s already exists", voteAddress)
	}

	dc.voteAccounts[voteAddress] = &VoteAccount{
		VoteAddress:     voteAddress,
		ValidatorKey:    validatorKey,
		AuthorizedVoter: authorizedVoter,
		LastVoteSlot:    0,
		RootSlot:        0,
		Credits:         0,
		EpochCredits:    make([]EpochCredit, 0),
		CreatedAt:       time.Now(),
	}

	return nil
}

// AddDynamicVote adds a dynamic vote with PoS features
func (dc *DynamicCollector) AddDynamicVote(vote *Vote, voteAccount string, slotRange []uint64, rootSlot uint64) (bool, bool, error) {
	if err := vote.Validate(); err != nil {
		return false, false, err
	}

	if len(slotRange) == 0 {
		slotRange = []uint64{vote.Slot}
	}

	// Validate slot range
	if len(slotRange) > int(dc.maxSlotRange) {
		return false, false, fmt.Errorf("slot range too large: %d > %d", len(slotRange), dc.maxSlotRange)
	}

	// Sort slot range
	sort.Slice(slotRange, func(i, j int) bool {
		return slotRange[i] < slotRange[j]
	})

	dc.mu.Lock()
	defer dc.mu.Unlock()

	// Get validator stake
	stake, exists := dc.validatorStakes[vote.VoterID]
	if !exists {
		return false, false, fmt.Errorf("validator %s not found", vote.VoterID)
	}

	// Get or validate vote account
	voteAcc, exists := dc.voteAccounts[voteAccount]
	if !exists {
		return false, false, fmt.Errorf("vote account %s not found", voteAccount)
	}

	if voteAcc.ValidatorKey != vote.VoterID {
		return false, false, fmt.Errorf("vote account %s not authorized for validator %s", voteAccount, vote.VoterID)
	}

	// Create dynamic vote
	dynamicVote := &DynamicVote{
		Vote:           vote,
		VoteAccount:    voteAccount,
		ValidatorStake: stake,
		SlotRange:      slotRange,
		RootSlot:       rootSlot,
		Credits:        uint64(len(slotRange)), // 1 credit per slot voted
		LandedSlots:    make([]VoteLandedSlots, 0),
		State:          VoteStatePending,
		CreatedAt:      time.Now(),
	}

	// Add vote for each slot in range
	committed := false
	needApply := false

	for _, slot := range slotRange {
		slotVotes, ok := dc.votes[slot]
		if !ok {
			slotVotes = make(map[string]*DynamicVote)
			dc.votes[slot] = slotVotes
		}

		// Check for duplicate vote
		if _, exists := slotVotes[vote.VoterID]; exists {
			continue // Skip duplicate votes
		}

		slotVotes[vote.VoterID] = dynamicVote

		// Check if slot reaches consensus
		if dc.checkConsensus(slot) {
			committed = true
			if !dc.wasAlreadyCommitted(slot) {
				needApply = true
			}
		}
	}

	// Update vote account
	now := time.Now()
	voteAcc.LastVoteSlot = vote.Slot
	voteAcc.LastVoteHash = vote.BlockHash
	if rootSlot > voteAcc.RootSlot {
		voteAcc.RootSlot = rootSlot
	}

	// Mark vote as processed
	dynamicVote.State = VoteStateValid
	dynamicVote.ProcessedAt = &now

	return committed, needApply, nil
}

// checkConsensus checks if a slot has reached voting consensus
func (dc *DynamicCollector) checkConsensus(slot uint64) bool {
	slotVotes, exists := dc.votes[slot]
	if !exists {
		return false
	}

	stakeVoted := uint64(0)
	for _, vote := range slotVotes {
		if vote.State == VoteStateValid || vote.State == VoteStatePending {
			stakeVoted += vote.ValidatorStake
		}
	}

	requiredStake := uint64(float64(dc.totalStake) * dc.threshold)
	return stakeVoted >= requiredStake
}

// wasAlreadyCommitted checks if slot was already committed before
func (dc *DynamicCollector) wasAlreadyCommitted(slot uint64) bool {
	// This is a simplified check - in a real system you'd track committed slots
	return false
}

// GetSlotVotes returns all votes for a specific slot
func (dc *DynamicCollector) GetSlotVotes(slot uint64) map[string]*DynamicVote {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	result := make(map[string]*DynamicVote)
	if slotVotes, exists := dc.votes[slot]; exists {
		for voterID, vote := range slotVotes {
			result[voterID] = vote
		}
	}
	return result
}

// GetVoteAccount returns vote account information
func (dc *DynamicCollector) GetVoteAccount(voteAddress string) (*VoteAccount, bool) {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	voteAcc, exists := dc.voteAccounts[voteAddress]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid race conditions
	return &VoteAccount{
		VoteAddress:     voteAcc.VoteAddress,
		ValidatorKey:    voteAcc.ValidatorKey,
		AuthorizedVoter: voteAcc.AuthorizedVoter,
		LastVoteSlot:    voteAcc.LastVoteSlot,
		LastVoteHash:    voteAcc.LastVoteHash,
		RootSlot:        voteAcc.RootSlot,
		Credits:         voteAcc.Credits,
		EpochCredits:    append([]EpochCredit{}, voteAcc.EpochCredits...),
		CreatedAt:       voteAcc.CreatedAt,
	}, true
}

// GetValidatorStake returns the current stake for a validator
func (dc *DynamicCollector) GetValidatorStake(validatorKey string) (uint64, bool) {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	stake, exists := dc.validatorStakes[validatorKey]
	return stake, exists
}

// GetTotalStake returns the total active stake
func (dc *DynamicCollector) GetTotalStake() uint64 {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return dc.totalStake
}

// GetConsensusProgress returns consensus progress for a slot
func (dc *DynamicCollector) GetConsensusProgress(slot uint64) (float64, uint64, uint64) {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	slotVotes, exists := dc.votes[slot]
	if !exists {
		return 0.0, 0, dc.totalStake
	}

	stakeVoted := uint64(0)
	for _, vote := range slotVotes {
		if vote.State == VoteStateValid || vote.State == VoteStatePending {
			stakeVoted += vote.ValidatorStake
		}
	}

	progress := float64(stakeVoted) / float64(dc.totalStake)
	return progress, stakeVoted, dc.totalStake
}

// CleanupExpiredVotes removes expired votes to prevent memory leaks
func (dc *DynamicCollector) CleanupExpiredVotes() {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	now := time.Now()
	for slot, slotVotes := range dc.votes {
		for voterID, vote := range slotVotes {
			if now.Sub(vote.CreatedAt) > dc.voteTimeout {
				vote.State = VoteStateExpired
				delete(slotVotes, voterID)
			}
		}

		// Remove empty slot maps
		if len(slotVotes) == 0 {
			delete(dc.votes, slot)
		}
	}
}

// UpdateEpochInfo updates the current epoch information
func (dc *DynamicCollector) UpdateEpochInfo(epochInfo *EpochInfo) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.epochInfo = epochInfo
}

// ProcessEpochEnd processes end of epoch and awards vote credits
func (dc *DynamicCollector) ProcessEpochEnd(epoch uint64) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	// Award vote credits to validators
	for _, voteAcc := range dc.voteAccounts {
		// Calculate credits earned this epoch
		creditsEarned := uint64(0)
		// In a real implementation, you'd calculate based on actual voting performance
		// For now, we'll use a simplified approach

		previousCredits := voteAcc.Credits
		voteAcc.Credits += creditsEarned

		// Add to epoch credits history
		epochCredit := EpochCredit{
			Epoch:           epoch,
			Credits:         creditsEarned,
			PreviousCredits: previousCredits,
		}
		voteAcc.EpochCredits = append(voteAcc.EpochCredits, epochCredit)
	}
}
