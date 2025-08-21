package cmd

import (
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/staking"
)

// ConvertStakingConfig converts config.StakingConfig to staking.StakeManagerConfig
// This function is placed in cmd package to avoid import cycles
func ConvertStakingConfig(stakingCfg *config.StakingConfig) (*staking.StakeManagerConfig, error) {
	minStake, ok := new(big.Int).SetString(stakingCfg.MinStakeAmount, 10)
	if !ok {
		return nil, fmt.Errorf("invalid min_stake_amount: %s", stakingCfg.MinStakeAmount)
	}

	return &staking.StakeManagerConfig{
		SlotsPerEpoch:  stakingCfg.SlotsPerEpoch,
		MinStakeAmount: minStake,
		MaxValidators:  stakingCfg.MaxValidators,
	}, nil
}

// StartDynamicVotingSystem starts the automatic dynamic voting system
func StartDynamicVotingSystem(validator *staking.StakeValidator, stakeManager *staking.StakeManager) {
	log.Println("🗳️  Starting Dynamic Voting System...")

	// Get active validators count
	activeValidators := stakeManager.GetActiveValidators()
	validatorCount := len(activeValidators)

	log.Printf("📊 Found %d active validators for dynamic voting", validatorCount)

	if validatorCount < 1 {
		log.Println("⚠️  No active validators found, dynamic voting will activate when validators join")
		return
	}

	// Configure voting parameters based on validator count
	voteManager := validator.GetVoteManager()

	// Set quorum threshold: minimum 51% but adjust for small networks
	var quorumPercent int64 = 51
	if validatorCount <= 3 {
		quorumPercent = 67 // 2/3 for small networks
	}
	voteManager.SetQuorumThreshold(big.NewInt(quorumPercent))

	// Set voting duration based on network size
	votingDuration := 5 * time.Minute
	if validatorCount <= 3 {
		votingDuration = 2 * time.Minute // Faster for small networks
	}
	voteManager.SetVotingDuration(votingDuration)

	log.Printf("✅ Dynamic Voting configured:")
	log.Printf("   - Validators: %d", validatorCount)
	log.Printf("   - Quorum: %d%%", quorumPercent)
	log.Printf("   - Vote Duration: %v", votingDuration)

	// Start automatic periodic voting for epoch transitions
	startAutomaticEpochVoting(validator, stakeManager)

	// Start block validation monitoring
	startBlockVotingMonitor(validator)

	log.Println("🚀 Dynamic Voting System is now active!")
}

// startAutomaticEpochVoting starts periodic epoch transition voting
func startAutomaticEpochVoting(validator *staking.StakeValidator, stakeManager *staking.StakeManager) {
	go func() {
		ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
		defer ticker.Stop()

		var lastEpoch uint64 = 0

		for range ticker.C {
			currentSlot := validator.GetCurrentSlot()
			currentEpoch := currentSlot / 8640 // Assuming 8640 slots per epoch

			if currentEpoch > lastEpoch && currentEpoch > 0 {
				// New epoch detected, initiate voting
				log.Printf("🗳️  Auto-initiating epoch transition vote for epoch %d", currentEpoch)

				err := validator.InitiateEpochVote(currentEpoch)
				if err != nil {
					log.Printf("⚠️  Failed to initiate epoch vote: %v", err)
				} else {
					log.Printf("✅ Epoch %d voting initiated successfully", currentEpoch)
				}

				lastEpoch = currentEpoch
			}
		}
	}()
}

// startBlockVotingMonitor starts monitoring for block validation opportunities
func startBlockVotingMonitor(validator *staking.StakeValidator) {
	go func() {
		ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
		defer ticker.Stop()

		log.Println("📡 Block voting monitor started")

		for range ticker.C {
			// Check for active voting rounds that need participation
			activeVotes := validator.GetActiveVotes()

			for roundID, round := range activeVotes {
				// Check if we haven't voted yet and if it's a block vote
				if _, hasVoted := round.Votes[validator.Pubkey]; !hasVoted && round.VoteType == staking.VoteTypeBlock {
					// Auto-participate in block validation with a slight delay for validation
					go func(rID string, target string) {
						time.Sleep(1 * time.Second) // Allow time for block validation

						// Simple validation - in production this would be comprehensive
						isValid := len(target) > 0 // Basic check

						err := validator.CastVote(rID, isValid)
						if err != nil {
							log.Printf("⚠️  Auto-vote failed for %s: %v", rID, err)
						} else {
							log.Printf("🗳️  Auto-voted %t on block %s", isValid, target[:8])
						}
					}(roundID, round.Target)
				}
			}
		}
	}()
}

// MonitorValidatorActivity monitors validator participation in voting
func MonitorValidatorActivity(validator *staking.StakeValidator) {
	go func() {
		ticker := time.NewTicker(1 * time.Minute) // Monitor every minute
		defer ticker.Stop()

		log.Println("📊 Validator activity monitor started")

		for range ticker.C {
			activeVotes := validator.GetActiveVotes()
			if len(activeVotes) > 0 {
				log.Printf("📈 Voting Activity Summary:")
				log.Printf("   - Active voting rounds: %d", len(activeVotes))

				for roundID, round := range activeVotes {
					voteCount := len(round.Votes)
					log.Printf("   - %s: %d votes cast", roundID, voteCount)
				}
			}
		}
	}()
}

// AdaptVotingParameters automatically adjusts voting parameters based on network conditions
func AdaptVotingParameters(validator *staking.StakeValidator, stakeManager *staking.StakeManager) {
	go func() {
		ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
		defer ticker.Stop()

		log.Println("⚙️  Adaptive voting parameter monitor started")

		for range ticker.C {
			activeValidators := stakeManager.GetActiveValidators()
			validatorCount := len(activeValidators)

			if validatorCount == 0 {
				continue
			}

			voteManager := validator.GetVoteManager()

			// Dynamic quorum adjustment
			var newQuorum int64 = 51
			switch {
			case validatorCount == 1:
				newQuorum = 100 // Single validator needs 100%
			case validatorCount <= 3:
				newQuorum = 67 // Small network needs 2/3
			case validatorCount <= 10:
				newQuorum = 60 // Medium network 60%
			default:
				newQuorum = 51 // Large network 51%
			}

			voteManager.SetQuorumThreshold(big.NewInt(newQuorum))

			// Dynamic voting duration adjustment
			var newDuration time.Duration
			switch {
			case validatorCount == 1:
				newDuration = 30 * time.Second // Very fast for single validator
			case validatorCount <= 3:
				newDuration = 1 * time.Minute // Fast for small networks
			case validatorCount <= 10:
				newDuration = 3 * time.Minute // Medium for growing networks
			default:
				newDuration = 5 * time.Minute // Standard for large networks
			}

			voteManager.SetVotingDuration(newDuration)
		}
	}()
}
