package staking

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/blockstore"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/p2p"
	"github.com/mezonai/mmn/poh"
	"github.com/mezonai/mmn/types"
)

const NoSlot = ^uint64(0)

// IsLeaderForSlot checks if a validator is leader for a specific slot
func (v *StakeValidator) IsLeaderForSlot(slot uint64) bool {
	return v.stakeManager.IsLeaderForSlot(v.Pubkey, slot)
}

// IsLeader checks if this validator is the leader for the current slot
func (v *StakeValidator) IsLeader(currentSlot uint64) bool {
	return v.stakeManager.IsLeaderForSlot(v.Pubkey, currentSlot)
}

// IsFollower checks if this validator is a follower for the current slot
func (v *StakeValidator) IsFollower(currentSlot uint64) bool {
	return !v.IsLeader(currentSlot)
}

// GetCurrentSlot returns the current slot from the PoH recorder
func (v *StakeValidator) GetCurrentSlot() uint64 {
	return v.Recorder.CurrentSlot()
}

// GetValidatorStatus returns detailed status information for debugging
func (v *StakeValidator) GetValidatorStatus() map[string]interface{} {
	currentSlot := v.GetCurrentSlot()
	isLeader := v.IsLeader(currentSlot)
	isActive := v.isActiveValidator()

	status := map[string]interface{}{
		"pubkey":       v.Pubkey,
		"current_slot": currentSlot,
		"is_leader":    isLeader,
		"is_active":    isActive,
		"performance":  v.GetPerformanceMetrics(),
	}

	// Add stake information if available
	if stakeInfo, exists := v.GetStakeInfo(); exists {
		status["stake_amount"] = stakeInfo.StakeAmount.String()
		status["state"] = stakeInfo.State
		status["activation_slot"] = stakeInfo.ActivationSlot
	}

	// Add leader schedule info
	schedule := v.stakeManager.GetCurrentLeaderSchedule()
	if schedule != nil {
		if leader, exists := schedule.LeaderAt(currentSlot); exists {
			status["current_leader"] = leader
		}
		// Count total schedule entries
		scheduleEntries := schedule.LeadersInRange(0, ^uint64(0)) // All slots
		status["schedule_entries"] = len(scheduleEntries)
	}

	return status
}

// StakeValidator represents the original Validator with PoS capabilities
type StakeValidator struct {
	// Core identity
	Pubkey  string
	PrivKey ed25519.PrivateKey

	// PoH integration
	Recorder *poh.PohRecorder
	Service  *poh.PohService

	// Blockchain components
	Mempool    *mempool.Mempool
	netClient  interfaces.Broadcaster
	blockStore blockstore.Store
	ledger     *ledger.Ledger
	collector  *consensus.Collector

	// PoS integration
	stakeManager *StakeManager

	// Dynamic voting integration
	voteManager *DynamicVoteManager

	// Configuration parameters
	TicksPerSlot              uint64
	BatchSize                 int
	leaderBatchLoopInterval   time.Duration
	roleMonitorLoopInterval   time.Duration
	leaderTimeout             time.Duration
	leaderTimeoutLoopInterval time.Duration

	// Session management
	session     *ledger.Session
	lastSession *ledger.Session

	// State tracking
	lastSlot          uint64
	leaderStartAtSlot uint64
	collectedEntries  []poh.Entry
	stopCh            chan struct{}

	// Performance tracking (no rewards)
	slotsProduced   uint64
	blocksProduced  uint64
	slotsMissed     uint64
	lastPerformance float64
}

// NewStakeValidator creates a new stake-enabled validator
func NewStakeValidator(
	pubkey string,
	privKey ed25519.PrivateKey,
	rec *poh.PohRecorder,
	svc *poh.PohService,
	mempool *mempool.Mempool,
	ticksPerSlot uint64,
	leaderBatchLoopInterval time.Duration,
	roleMonitorLoopInterval time.Duration,
	leaderTimeout time.Duration,
	leaderTimeoutLoopInterval time.Duration,
	batchSize int,
	p2pClient *p2p.Libp2pNetwork,
	blockStore blockstore.Store,
	ledger *ledger.Ledger,
	collector *consensus.Collector,
	stakeManager *StakeManager,
) *StakeValidator {
	// Validate required dependencies
	if rec == nil {
		panic("PohRecorder cannot be nil")
	}
	if mempool == nil {
		panic("Mempool cannot be nil")
	}
	if stakeManager == nil {
		panic("StakeManager cannot be nil")
	}
	if ledger == nil {
		panic("Ledger cannot be nil")
	}

	v := &StakeValidator{
		Pubkey:                    pubkey,
		PrivKey:                   privKey,
		Recorder:                  rec,
		Service:                   svc,
		Mempool:                   mempool,
		TicksPerSlot:              ticksPerSlot,
		leaderBatchLoopInterval:   leaderBatchLoopInterval,
		roleMonitorLoopInterval:   roleMonitorLoopInterval,
		leaderTimeout:             leaderTimeout,
		leaderTimeoutLoopInterval: leaderTimeoutLoopInterval,
		BatchSize:                 batchSize,
		netClient:                 p2pClient,
		blockStore:                blockStore,
		ledger:                    ledger,
		session:                   ledger.NewSession(),
		lastSession:               ledger.NewSession(),
		lastSlot:                  0,
		leaderStartAtSlot:         NoSlot,
		collectedEntries:          make([]poh.Entry, 0),
		collector:                 collector,
		stakeManager:              stakeManager,
		voteManager:               NewDynamicVoteManager(stakeManager, collector),
		stopCh:                    make(chan struct{}),
	}
	svc.OnEntry = v.handleEntry
	return v
}

// Run starts the validator
func (v *StakeValidator) Run() {
	go v.leaderBatchLoop()
	go v.roleMonitorLoop()
	go v.leaderTimeoutLoop()
	go v.performanceTracker()
}

// Stop stops the validator
func (v *StakeValidator) Stop() {
	close(v.stopCh)
}

// handleEntry processes entries from PoH service
func (v *StakeValidator) handleEntry(entries []poh.Entry) {
	v.collectedEntries = append(v.collectedEntries, entries...)

	currentSlot := v.Recorder.CurrentSlot()

	// Process any blocks in the entries for voting
	for _, entry := range entries {
		if len(entry.Transactions) > 0 || !entry.Tick {
			// Check if this contains substantial transaction data for voting
			v.processEntryForVoting(entry, currentSlot)
		}
	}

	// Use stake manager to check leadership
	shouldBeLeader := v.stakeManager.IsLeaderForSlot(v.Pubkey, currentSlot)

	if shouldBeLeader {
		if v.leaderStartAtSlot != currentSlot {
			// New leader slot or reassignment
			if v.leaderStartAtSlot != NoSlot {
				logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Transitioning from leader slot %d to %d",
					v.leaderStartAtSlot, currentSlot))
			}
			v.onLeaderSlotStart(currentSlot)
		}
		v.onLeaderSlotTick(currentSlot)
	} else {
		v.onFollowerSlot(currentSlot)
	}
}

// processEntryForVoting checks entries for blocks and triggers voting
func (v *StakeValidator) processEntryForVoting(entry poh.Entry, currentSlot uint64) {
	// In a real implementation, this would parse the entry data to extract blocks
	// For now, we'll simulate block detection based on transactions
	if len(entry.Transactions) > 0 { // Entries with transactions are considered for voting
		entryHashStr := fmt.Sprintf("%x", entry.Hash[:8]) // Use entry hash as block identifier

		// Trigger automatic voting on this entry/block
		go v.AutoVoteOnBlocks(entryHashStr, v.validateEntry(entry))
	}
}

// validateEntry performs basic validation on entry data
func (v *StakeValidator) validateEntry(entry poh.Entry) bool {
	// Simplified validation - in practice this would be comprehensive
	// Check transaction validity, hash consistency, etc.
	return len(entry.Transactions) >= 0 // Always valid for now
}

// onLeaderSlotStart initializes leader slot
func (v *StakeValidator) onLeaderSlotStart(currentSlot uint64) {
	v.leaderStartAtSlot = currentSlot
	logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Starting leader slot %d", currentSlot))

	// Generate stake proof for this slot
	proof, err := v.stakeManager.stakePool.GenerateStakeProof(v.Pubkey, currentSlot, v.PrivKey)
	if err != nil {
		logx.Error("STAKE_VALIDATOR", "Failed to generate stake proof:", err)
		return
	}

	logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Generated stake proof for slot %d: %x", currentSlot, proof[:8]))
}

// onLeaderSlotTick processes leader slot tick
func (v *StakeValidator) onLeaderSlotTick(currentSlot uint64) {
	// Collect transactions from mempool
	var txs [][]byte
	if v.Mempool != nil {
		txs = v.Mempool.PullBatch(v.BatchSize)
	}
	logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Leader tick slot %d: collected %d transactions", currentSlot, len(txs)))

	// Include staking transactions
	stakeTxs := v.getStakeTransactionsFromMempool()
	if len(stakeTxs) > 0 {
		logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Including %d stake transactions", len(stakeTxs)))
		// Convert stake transactions to regular transactions for inclusion
		for _, stakeTx := range stakeTxs {
			serialized, err := SerializeTransaction(stakeTx)
			if err == nil {
				txs = append(txs, serialized)
			}
		}
	}

	if len(txs) > 0 {
		// Convert [][]byte to []*types.Transaction
		transactions := make([]*types.Transaction, 0, len(txs))
		for _, txData := range txs {
			var tx types.Transaction
			if err := json.Unmarshal(txData, &tx); err == nil {
				transactions = append(transactions, &tx)
			}
		}

		// Record transactions with PoH
		if len(transactions) > 0 && v.Recorder != nil {
			entry, err := v.Recorder.RecordTxs(transactions)
			if err == nil && entry != nil {
				v.collectedEntries = append(v.collectedEntries, *entry)
				logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Recorded %d transactions in entry", len(transactions)))
			} else {
				logx.Error("STAKE_VALIDATOR", "Failed to record transactions:", err)
			}
		}
	} else {
		// No transactions, but we should still tick the PoH to create empty entries
		if v.Recorder != nil {
			entry := v.Recorder.Tick()
			if entry != nil {
				v.collectedEntries = append(v.collectedEntries, *entry)
				logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Created tick entry for empty slot %d", currentSlot))
			} else {
				// If Recorder.Tick() returns nil, create a minimal tick entry manually
				logx.Info("STAKE_VALIDATOR", "Creating manual tick entry for slot progression")
				tickEntry := poh.Entry{
					Tick:         true,
					Hash:         [32]byte{}, // Empty hash for tick
					Transactions: []*types.Transaction{},
				}
				v.collectedEntries = append(v.collectedEntries, tickEntry)
			}
		} else {
			logx.Warn("STAKE_VALIDATOR", "Recorder is nil, cannot create tick entry")
		}
	}

	// Check if slot is complete
	if v.isSlotComplete(currentSlot) {
		logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Slot %d is complete with %d entries, finalizing...", currentSlot, len(v.collectedEntries)))
		v.finalizeLeaderSlot(currentSlot)
	} else {
		logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Slot %d not complete yet (%d entries)", currentSlot, len(v.collectedEntries)))
	}
}

// onFollowerSlot processes follower slot
func (v *StakeValidator) onFollowerSlot(currentSlot uint64) {
	// Wait for block from leader and validate
	// In the current implementation, this is handled by the network layer

	// Track slot completion for performance metrics
	if currentSlot > v.lastSlot {
		v.lastSlot = currentSlot
		// Notify stake manager of slot completion
		v.stakeManager.OnSlotComplete(currentSlot, "", true)
	}
}

// finalizeLeaderSlot completes block production for leader slot
func (v *StakeValidator) finalizeLeaderSlot(slot uint64) {
	logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Finalizing leader slot %d with %d entries",
		slot, len(v.collectedEntries)))

	// Create block from collected entries
	block := v.createBlockFromEntries(slot, v.collectedEntries)

	// Apply block to ledger session
	session := v.ledger.NewSession()
	err := v.applyBlockToSession(block, session)
	if err != nil {
		logx.Error("STAKE_VALIDATOR", "Failed to apply block to session:", err)
		v.slotsMissed++
		v.resetLeaderSlot()
		return
	}

	// Commit session
	v.session = session

	// Store block
	err = v.blockStore.AddBlockPending(block)
	if err != nil {
		logx.Error("STAKE_VALIDATOR", "Failed to store block:", err)
		v.slotsMissed++
		v.resetLeaderSlot()
		return
	}

	// Broadcast block to network
	ctx := context.Background()
	err = v.netClient.BroadcastBlock(ctx, block)
	if err != nil {
		logx.Error("STAKE_VALIDATOR", "Failed to broadcast block:", err)
	}

	// Initiate voting for this block (dynamic voting)
	blockHashStr := fmt.Sprintf("%x", block.Hash)
	if err := v.InitiateBlockVote(blockHashStr); err != nil {
		logx.Error("STAKE_VALIDATOR", fmt.Sprintf("Failed to initiate block vote for %s: %v", blockHashStr[:8], err))
	} else {
		logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Initiated voting for block %s", blockHashStr[:8]))
	}

	// Update performance metrics
	v.blocksProduced++
	v.slotsProduced++

	// Notify stake manager of successful slot completion
	v.stakeManager.OnSlotComplete(slot, v.Pubkey, true)

	// Reset for next slot
	v.resetLeaderSlot()

	logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Successfully produced block for slot %d", slot))
}

// Background loops

func (v *StakeValidator) leaderBatchLoop() {
	ticker := time.NewTicker(v.leaderBatchLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			v.processPendingTransactions()
		}
	}
}

func (v *StakeValidator) roleMonitorLoop() {
	ticker := time.NewTicker(v.roleMonitorLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			v.monitorRole()
		}
	}
}

func (v *StakeValidator) leaderTimeoutLoop() {
	ticker := time.NewTicker(v.leaderTimeoutLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			v.checkLeaderTimeout()
		}
	}
}

func (v *StakeValidator) performanceTracker() {
	ticker := time.NewTicker(30 * time.Second) // Update every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			v.updatePerformanceMetrics()
			v.distributeRewards()
		}
	}
}

// updatePerformanceMetrics calculates current performance ratio
func (v *StakeValidator) updatePerformanceMetrics() {
	totalSlots := v.slotsProduced + v.slotsMissed
	if totalSlots == 0 {
		v.lastPerformance = 1.0
		return
	}

	v.lastPerformance = float64(v.slotsProduced) / float64(totalSlots)

	logx.Info("STAKE_VALIDATOR", fmt.Sprintf(
		"Performance update - Produced: %d, Missed: %d, Ratio: %.2f%%",
		v.slotsProduced, v.slotsMissed, v.lastPerformance*100,
	))
}

// distributeRewards removed - no reward system
func (v *StakeValidator) distributeRewards() {
	// No reward distribution - removed by request
} // GetPerformanceStats returns current validator performance statistics (no rewards)
func (v *StakeValidator) GetPerformanceStats() (uint64, uint64, uint64, float64) {
	return v.slotsProduced, v.blocksProduced, v.slotsMissed, v.lastPerformance
}

// Helper methods

func (v *StakeValidator) getStakeTransactionsFromMempool() []*StakeTransaction {
	// In a real implementation, this would extract stake transactions from mempool
	// For now, return empty slice
	return []*StakeTransaction{}
}

func (v *StakeValidator) isSlotComplete(currentSlot uint64) bool {
	// Check if we have enough entries or reached slot boundary
	if len(v.collectedEntries) >= v.BatchSize {
		return true
	}

	// Force complete slot if we've been leader for too long (next slot started)
	nextSlot := v.Recorder.CurrentSlot()
	if nextSlot > currentSlot {
		return true
	}

	// Also complete if we have at least some entries and slot has progressed
	if len(v.collectedEntries) > 0 && v.leaderStartAtSlot != NoSlot && currentSlot > v.leaderStartAtSlot {
		return true
	}

	// For empty slots, complete after reasonable time
	return v.leaderStartAtSlot != NoSlot && nextSlot > v.leaderStartAtSlot
}

func (v *StakeValidator) createBlockFromEntries(slot uint64, entries []poh.Entry) *block.BroadcastedBlock {
	// Use AssembleBlock function to create block properly
	b := block.AssembleBlock(
		slot,
		[32]byte{}, // Should be actual parent hash
		v.Pubkey,
		entries,
	)

	// Set status to pending
	b.Status = block.BlockPending

	return b
}

func (v *StakeValidator) applyBlockToSession(block *block.BroadcastedBlock, session *ledger.Session) error {
	// Apply all transactions in block entries to the session
	for _, entry := range block.Entries {
		if !entry.Tick {
			for _, tx := range entry.Transactions {
				// Check if it's a stake transaction
				if stakeTx, err := DeserializeTransaction(tx.Bytes()); err == nil {
					// Process stake transaction
					err = v.processStakeTransaction(stakeTx, session)
					if err != nil {
						logx.Error("STAKE_VALIDATOR", "Failed to process stake transaction:", err)
						continue
					}
				}
				// Process regular transaction
				// tx, err := types.DeserializeTransaction(txData)
				// if err == nil {
				//     session.ApplyTransaction(tx)
				// }
			}
		}
	}
	return nil
}

func (v *StakeValidator) processStakeTransaction(stakeTx *StakeTransaction, session *ledger.Session) error {
	// Process stake transaction within ledger session
	// This would integrate with the transaction processor
	logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Processing %s transaction", stakeTx.Type.String()))
	return nil
}

func (v *StakeValidator) resetLeaderSlot() {
	v.collectedEntries = make([]poh.Entry, 0)
	v.leaderStartAtSlot = NoSlot
}

func (v *StakeValidator) processPendingTransactions() {
	// Process any pending transactions
	if v.leaderStartAtSlot != NoSlot {
		currentSlot := v.Recorder.CurrentSlot()
		if currentSlot == v.leaderStartAtSlot {
			v.onLeaderSlotTick(currentSlot)
		}
	}
}

func (v *StakeValidator) monitorRole() {
	currentSlot := v.Recorder.CurrentSlot()
	if currentSlot <= v.lastSlot {
		return // No new slot yet
	}

	v.lastSlot = currentSlot

	// Check if this validator should be leader for current slot
	isLeader := v.stakeManager.IsLeaderForSlot(v.Pubkey, currentSlot)

	// In single validator networks, this validator should always be leader
	if v.stakeManager.GetValidatorCount() <= 1 {
		isLeader = true
	}

	if isLeader {
		// Check if we're not already processing this slot as leader
		if v.leaderStartAtSlot != currentSlot {
			logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Assigned as leader for slot %d", currentSlot))
			v.onLeaderSlotStart(currentSlot)
		}
	} else {
		// We're a follower for this slot
		if v.leaderStartAtSlot != NoSlot && v.leaderStartAtSlot < currentSlot {
			// We were leader but slot moved on, reset leader state
			v.resetLeaderSlot()
		}
		v.onFollowerSlot(currentSlot)
	}
}

func (v *StakeValidator) checkLeaderTimeout() {
	if v.leaderStartAtSlot == NoSlot {
		return
	}

	currentSlot := v.Recorder.CurrentSlot()
	slotDiff := currentSlot - v.leaderStartAtSlot

	// Allow more time for single validator networks - timeout after 3 slots
	if slotDiff > 2 {
		logx.Warn("STAKE_VALIDATOR", fmt.Sprintf("Leader slot %d timeout (current slot: %d, diff: %d)",
			v.leaderStartAtSlot, currentSlot, slotDiff))
		v.slotsMissed++

		// For single validator networks, immediately try to become leader again
		if v.stakeManager.GetValidatorCount() <= 1 {
			logx.Info("STAKE_VALIDATOR", "Single validator network - reassigning leadership")
			oldSlot := v.leaderStartAtSlot
			v.resetLeaderSlot()

			// In single validator network, this validator should always be leader
			// Force assignment to current slot immediately
			logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Force assigning leadership for current slot %d (was slot %d)", currentSlot, oldSlot))
			v.onLeaderSlotStart(currentSlot)
		} else {
			v.resetLeaderSlot()
		}
	}
}

// Helper methods - performance tracking is handled above in performanceTracker()

// Public API methods

// GetPerformanceMetrics returns validator performance metrics
func (v *StakeValidator) GetPerformanceMetrics() map[string]interface{} {
	return map[string]interface{}{
		"slots_produced":   v.slotsProduced,
		"blocks_produced":  v.blocksProduced,
		"slots_missed":     v.slotsMissed,
		"performance_rate": v.lastPerformance,
		"is_active":        v.isActiveValidator(),
	}
}

// GetStakeInfo returns staking information for this validator
func (v *StakeValidator) GetStakeInfo() (*ValidatorInfo, bool) {
	return v.stakeManager.GetValidatorInfo(v.Pubkey)
}

func (v *StakeValidator) isActiveValidator() bool {
	validators := v.stakeManager.GetActiveValidators()
	_, exists := validators[v.Pubkey]
	return exists
}

// GetVoteManager returns the dynamic vote manager
func (v *StakeValidator) GetVoteManager() *DynamicVoteManager {
	return v.voteManager
}

// ========== Dynamic Voting Methods ==========

// CastVote allows this validator to cast a vote with their stake weight
func (v *StakeValidator) CastVote(roundID string, support bool) error {
	if !v.isActiveValidator() {
		return fmt.Errorf("validator %s is not active", v.Pubkey)
	}

	// Create vote signature
	voteData := fmt.Sprintf("%s:%t", roundID, support)
	signature := ed25519.Sign(v.PrivKey, []byte(voteData))

	return v.voteManager.CastVote(roundID, v.Pubkey, support, signature)
}

// InitiateBlockVote starts a voting round for block approval
func (v *StakeValidator) InitiateBlockVote(blockHash string) error {
	currentSlot := v.GetCurrentSlot()
	isLeader := v.IsLeader(currentSlot)

	// In single validator networks, always allow block vote initiation
	if v.stakeManager.GetValidatorCount() <= 1 {
		logx.Info("STAKE_VALIDATOR", "Single validator network: bypassing leader check for block vote")
		isLeader = true
	}

	if !isLeader {
		return fmt.Errorf("only leader can initiate block votes")
	}

	roundID := fmt.Sprintf("block_%s_%d", blockHash[:8], currentSlot)
	minStake := big.NewInt(0) // No minimum stake required for block votes

	return v.voteManager.StartVotingRound(roundID, VoteTypeBlock, blockHash, minStake)
}

// InitiateEpochVote starts a voting round for epoch transition
func (v *StakeValidator) InitiateEpochVote(epochNumber uint64) error {
	if !v.IsLeader(v.GetCurrentSlot()) {
		return fmt.Errorf("only leader can initiate epoch votes")
	}

	roundID := fmt.Sprintf("epoch_%d", epochNumber)
	totalStake := v.stakeManager.stakePool.GetTotalStake()
	minStake := new(big.Int).Mul(totalStake, big.NewInt(30)) // 30% of total stake
	minStake.Div(minStake, big.NewInt(100))

	return v.voteManager.StartVotingRound(roundID, VoteTypeEpoch, fmt.Sprintf("%d", epochNumber), minStake)
}

// InitiateSlashingVote starts a voting round for slashing a validator
func (v *StakeValidator) InitiateSlashingVote(targetValidator string, reason string) error {
	if !v.isActiveValidator() {
		return fmt.Errorf("only active validators can initiate slashing votes")
	}

	roundID := fmt.Sprintf("slash_%s_%d", targetValidator[:8], time.Now().Unix())
	totalStake := v.stakeManager.stakePool.GetTotalStake()
	minStake := new(big.Int).Mul(totalStake, big.NewInt(25)) // 25% of total stake
	minStake.Div(minStake, big.NewInt(100))

	target := fmt.Sprintf("%s:%s", targetValidator, reason)
	return v.voteManager.StartVotingRound(roundID, VoteTypeSlashing, target, minStake)
}

// GetActiveVotes returns all active voting rounds that this validator can participate in
func (v *StakeValidator) GetActiveVotes() map[string]*VotingRound {
	return v.voteManager.GetActiveVotingRounds()
}

// GetVoteStatus returns the status of a specific voting round
func (v *StakeValidator) GetVoteStatus(roundID string) (*VotingRound, bool) {
	return v.voteManager.GetVotingRoundStatus(roundID)
}

// AutoVoteOnBlocks automatically votes on block proposals based on validator's policy
func (v *StakeValidator) AutoVoteOnBlocks(blockHash string, isValid bool) {
	if !v.isActiveValidator() {
		return
	}

	// Look for active block voting rounds
	activeRounds := v.voteManager.GetActiveVotingRounds()
	for roundID, round := range activeRounds {
		if round.VoteType == VoteTypeBlock && round.Target == blockHash {
			// Check if we haven't voted yet
			if _, hasVoted := round.Votes[v.Pubkey]; !hasVoted {
				err := v.CastVote(roundID, isValid)
				if err != nil {
					logx.Error("DYNAMIC_VOTE", fmt.Sprintf("Failed to auto-vote on block %s: %v", blockHash[:8], err))
				} else {
					logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Auto-voted %t on block %s", isValid, blockHash[:8]))
				}
			}
		}
	}
}

// ProcessIncomingVoteRequest handles vote requests from other validators
func (v *StakeValidator) ProcessIncomingVoteRequest(fromValidator string, roundID string, voteType VoteType, target string) {
	if !v.isActiveValidator() {
		return
	}

	// Auto-participate in certain types of votes
	switch voteType {
	case VoteTypeBlock:
		// For block votes, validate the block and vote accordingly
		go v.validateAndVoteOnBlock(roundID, target)

	case VoteTypeEpoch:
		// For epoch votes, check if epoch transition is appropriate
		go v.validateAndVoteOnEpoch(roundID, target)

	case VoteTypeSlashing:
		// For slashing votes, require manual decision (no auto-vote)
		logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Slashing vote %s requires manual decision", roundID))
	}
}

// validateAndVoteOnBlock validates a block and votes automatically
func (v *StakeValidator) validateAndVoteOnBlock(roundID string, blockHash string) {
	// Simple validation - in practice this would be more sophisticated
	isValid := true // Placeholder - real validation logic would go here

	// Small delay to avoid voting too quickly
	time.Sleep(1 * time.Second)

	err := v.CastVote(roundID, isValid)
	if err != nil {
		logx.Error("DYNAMIC_VOTE", fmt.Sprintf("Failed to vote on block validation: %v", err))
	} else {
		logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Voted %t on block %s validation", isValid, blockHash[:8]))
	}
}

// validateAndVoteOnEpoch validates an epoch transition and votes
func (v *StakeValidator) validateAndVoteOnEpoch(roundID string, epochStr string) {
	// Check if epoch transition is appropriate
	// For now, always support epoch transitions
	isSupported := true

	time.Sleep(2 * time.Second) // Allow time for consideration

	err := v.CastVote(roundID, isSupported)
	if err != nil {
		logx.Error("DYNAMIC_VOTE", fmt.Sprintf("Failed to vote on epoch transition: %v", err))
	} else {
		logx.Info("DYNAMIC_VOTE", fmt.Sprintf("Voted %t on epoch %s transition", isSupported, epochStr))
	}
}
