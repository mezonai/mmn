package staking

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/logx"
)

// VoteType represents different types of votes
type VoteType int

const (
	VoteTypeBlock VoteType = iota
	VoteTypeEpoch
	VoteTypeSlashing
	VoteTypeGovernance
)

// DynamicVote represents a vote with stake weight
type DynamicVote struct {
	VoterPubKey   string
	VoteType      VoteType
	Target        string   // Block hash, epoch number, etc.
	Support       bool     // true for yes, false for no
	StakeWeight   *big.Int // Voting power based on stake
	Timestamp     time.Time
	VoteSignature []byte
}

// VotingRound represents a voting session
type VotingRound struct {
	RoundID       string
	VoteType      VoteType
	Target        string
	StartTime     time.Time
	EndTime       time.Time
	Votes         map[string]*DynamicVote // voterPubKey -> vote
	RequiredStake *big.Int                // Minimum total stake required
	Status        VotingStatus
}

type VotingStatus int

const (
	VotingActive VotingStatus = iota
	VotingPassed
	VotingFailed
	VotingExpired
)

// DynamicVoteManager manages dynamic voting with stake weights
type DynamicVoteManager struct {
	stakeManager  *StakeManager
	voteCollector *consensus.Collector

	// Voting rounds
	activeRounds map[string]*VotingRound // roundID -> round
	roundHistory []*VotingRound

	// Voting parameters
	votingDuration  time.Duration
	quorumThreshold *big.Int // Minimum stake percentage for quorum (out of 100)

	mu sync.RWMutex
}

// NewDynamicVoteManager creates a new dynamic vote manager
func NewDynamicVoteManager(stakeManager *StakeManager, voteCollector *consensus.Collector) *DynamicVoteManager {
	return &DynamicVoteManager{
		stakeManager:    stakeManager,
		voteCollector:   voteCollector,
		activeRounds:    make(map[string]*VotingRound),
		roundHistory:    make([]*VotingRound, 0),
		votingDuration:  5 * time.Minute, // Default 5 minutes
		quorumThreshold: big.NewInt(51),  // 51% quorum
	}
}

// StartVotingRound initiates a new voting round
func (dvm *DynamicVoteManager) StartVotingRound(roundID string, voteType VoteType, target string, requiredStake *big.Int) error {
	dvm.mu.Lock()
	defer dvm.mu.Unlock()

	if _, exists := dvm.activeRounds[roundID]; exists {
		return fmt.Errorf("voting round %s already active", roundID)
	}

	round := &VotingRound{
		RoundID:       roundID,
		VoteType:      voteType,
		Target:        target,
		StartTime:     time.Now(),
		EndTime:       time.Now().Add(dvm.votingDuration),
		Votes:         make(map[string]*DynamicVote),
		RequiredStake: requiredStake,
		Status:        VotingActive,
	}

	dvm.activeRounds[roundID] = round
	logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Started voting round %s for %s", roundID, target))

	// Start monitoring goroutine
	go dvm.monitorVotingRound(roundID)

	return nil
}

// CastVote allows a validator to cast a vote with their stake weight
func (dvm *DynamicVoteManager) CastVote(roundID, voterPubKey string, support bool, signature []byte) error {
	dvm.mu.Lock()
	defer dvm.mu.Unlock()

	round, exists := dvm.activeRounds[roundID]
	if !exists {
		return fmt.Errorf("voting round %s not found", roundID)
	}

	if round.Status != VotingActive {
		return fmt.Errorf("voting round %s is not active", roundID)
	}

	if time.Now().After(round.EndTime) {
		return fmt.Errorf("voting round %s has expired", roundID)
	}

	// Get validator's stake weight
	validatorInfo, exists := dvm.stakeManager.stakePool.GetValidatorInfo(voterPubKey)
	if !exists {
		return fmt.Errorf("validator %s not found", voterPubKey)
	}

	if validatorInfo.State != StakeStateActive {
		return fmt.Errorf("validator %s is not active", voterPubKey)
	}

	vote := &DynamicVote{
		VoterPubKey:   voterPubKey,
		VoteType:      round.VoteType,
		Target:        round.Target,
		Support:       support,
		StakeWeight:   new(big.Int).Set(validatorInfo.StakeAmount),
		Timestamp:     time.Now(),
		VoteSignature: signature,
	}

	round.Votes[voterPubKey] = vote
	logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Vote cast by %s with weight %s for round %s",
		voterPubKey[:8], validatorInfo.StakeAmount.String(), roundID))

	// Check if round should be concluded early
	dvm.checkVotingConclusion(round)

	return nil
}

// checkVotingConclusion checks if voting can be concluded early
func (dvm *DynamicVoteManager) checkVotingConclusion(round *VotingRound) {
	totalStake := dvm.stakeManager.stakePool.GetTotalStake()

	supportStake := big.NewInt(0)
	opposeStake := big.NewInt(0)
	totalVoted := big.NewInt(0)

	for _, vote := range round.Votes {
		totalVoted.Add(totalVoted, vote.StakeWeight)
		if vote.Support {
			supportStake.Add(supportStake, vote.StakeWeight)
		} else {
			opposeStake.Add(opposeStake, vote.StakeWeight)
		}
	}

	// Check quorum (enough stake has voted)
	quorumRequired := new(big.Int).Mul(totalStake, dvm.quorumThreshold)
	quorumRequired.Div(quorumRequired, big.NewInt(100))

	if totalVoted.Cmp(quorumRequired) >= 0 {
		// Determine result
		if supportStake.Cmp(opposeStake) > 0 {
			round.Status = VotingPassed
			logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Voting round %s PASSED early", round.RoundID))
		} else {
			round.Status = VotingFailed
			logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Voting round %s FAILED early", round.RoundID))
		}

		dvm.concludeVotingRound(round)
	}
}

// monitorVotingRound monitors a voting round for expiration
func (dvm *DynamicVoteManager) monitorVotingRound(roundID string) {
	for {
		time.Sleep(10 * time.Second)

		dvm.mu.RLock()
		round, exists := dvm.activeRounds[roundID]
		if !exists {
			dvm.mu.RUnlock()
			return // Round was concluded
		}

		if time.Now().After(round.EndTime) && round.Status == VotingActive {
			dvm.mu.RUnlock()
			dvm.mu.Lock()
			dvm.finalizeExpiredRound(round)
			dvm.mu.Unlock()
			return
		}
		dvm.mu.RUnlock()
	}
}

// finalizeExpiredRound finalizes a voting round that has expired
func (dvm *DynamicVoteManager) finalizeExpiredRound(round *VotingRound) {
	totalStake := dvm.stakeManager.stakePool.GetTotalStake()

	supportStake := big.NewInt(0)
	opposeStake := big.NewInt(0)
	totalVoted := big.NewInt(0)

	for _, vote := range round.Votes {
		totalVoted.Add(totalVoted, vote.StakeWeight)
		if vote.Support {
			supportStake.Add(supportStake, vote.StakeWeight)
		} else {
			opposeStake.Add(opposeStake, vote.StakeWeight)
		}
	}

	// Check quorum
	quorumRequired := new(big.Int).Mul(totalStake, dvm.quorumThreshold)
	quorumRequired.Div(quorumRequired, big.NewInt(100))

	if totalVoted.Cmp(quorumRequired) < 0 {
		round.Status = VotingExpired
		logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Voting round %s EXPIRED - insufficient quorum", round.RoundID))
	} else {
		if supportStake.Cmp(opposeStake) > 0 {
			round.Status = VotingPassed
			logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Voting round %s PASSED", round.RoundID))
		} else {
			round.Status = VotingFailed
			logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Voting round %s FAILED", round.RoundID))
		}
	}

	dvm.concludeVotingRound(round)
}

// concludeVotingRound moves a round from active to history
func (dvm *DynamicVoteManager) concludeVotingRound(round *VotingRound) {
	delete(dvm.activeRounds, round.RoundID)
	dvm.roundHistory = append(dvm.roundHistory, round)

	// Execute voting result if applicable
	dvm.executeVotingResult(round)
}

// executeVotingResult executes the result of a voting round
func (dvm *DynamicVoteManager) executeVotingResult(round *VotingRound) {
	if round.Status != VotingPassed {
		return
	}

	switch round.VoteType {
	case VoteTypeBlock:
		logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Block %s approved by vote", round.Target))
		// Additional block approval logic can be added here

	case VoteTypeEpoch:
		logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Epoch transition %s approved", round.Target))
		// Epoch transition logic

	case VoteTypeSlashing:
		logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Slashing proposal %s approved", round.Target))
		// Slashing execution logic

	case VoteTypeGovernance:
		logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Governance proposal %s approved", round.Target))
		// Governance change logic
	}
}

// GetVotingRoundStatus returns the current status of a voting round
func (dvm *DynamicVoteManager) GetVotingRoundStatus(roundID string) (*VotingRound, bool) {
	dvm.mu.RLock()
	defer dvm.mu.RUnlock()

	if round, exists := dvm.activeRounds[roundID]; exists {
		return round, true
	}

	// Check history
	for _, round := range dvm.roundHistory {
		if round.RoundID == roundID {
			return round, true
		}
	}

	return nil, false
}

// GetActiveVotingRounds returns all currently active voting rounds
func (dvm *DynamicVoteManager) GetActiveVotingRounds() map[string]*VotingRound {
	dvm.mu.RLock()
	defer dvm.mu.RUnlock()

	// Return a copy to avoid concurrent modifications
	rounds := make(map[string]*VotingRound)
	for k, v := range dvm.activeRounds {
		rounds[k] = v
	}
	return rounds
}

// SetQuorumThreshold sets the minimum stake percentage required for quorum
func (dvm *DynamicVoteManager) SetQuorumThreshold(threshold *big.Int) {
	dvm.mu.Lock()
	defer dvm.mu.Unlock()

	if threshold.Cmp(big.NewInt(0)) > 0 && threshold.Cmp(big.NewInt(100)) <= 0 {
		dvm.quorumThreshold = new(big.Int).Set(threshold)
		logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Quorum threshold set to %s%%", threshold.String()))
	}
}

// SetVotingDuration sets the duration for voting rounds
func (dvm *DynamicVoteManager) SetVotingDuration(duration time.Duration) {
	dvm.mu.Lock()
	defer dvm.mu.Unlock()

	dvm.votingDuration = duration
	logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Voting duration set to %v", duration))
}
