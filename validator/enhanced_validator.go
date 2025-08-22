package validator

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/types"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/blockstore"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/p2p"
	"github.com/mezonai/mmn/poh"
)

// EnhancedValidator uses dynamic scheduling with PoS
type EnhancedValidator struct {
	Pubkey          string
	PrivKey         ed25519.PrivateKey
	Recorder        *poh.PohRecorder
	Service         *poh.PohService
	DynamicSchedule *poh.DynamicLeaderSchedule // Use dynamic scheduler
	Mempool         *mempool.Mempool
	TicksPerSlot    uint64

	// Configurable parameters
	leaderBatchLoopInterval   time.Duration
	roleMonitorLoopInterval   time.Duration
	leaderTimeout             time.Duration
	leaderTimeoutLoopInterval time.Duration
	BatchSize                 int

	netClient        interfaces.Broadcaster
	blockStore       blockstore.Store
	ledger           *ledger.Ledger
	session          *ledger.Session
	lastSession      *ledger.Session
	dynamicCollector *consensus.DynamicCollector // Use dynamic collector

	// Enhanced features
	voteAccount    string    // This validator's vote account
	validatorStake uint64    // Current stake amount
	epochStartTime time.Time // When current epoch started

	// Slot & entry buffer
	lastSlot          uint64
	leaderStartAtSlot uint64
	collectedEntries  []poh.Entry
	pendingValidTxs   []*types.Transaction
	stopCh            chan struct{}
}

// NewEnhancedValidator creates a validator with dynamic PoS features
func NewEnhancedValidator(
	pubkey string,
	privKey ed25519.PrivateKey,
	rec *poh.PohRecorder,
	svc *poh.PohService,
	dynamicSchedule *poh.DynamicLeaderSchedule,
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
	dynamicCollector *consensus.DynamicCollector,
	voteAccount string,
	validatorStake uint64,
) *EnhancedValidator {
	v := &EnhancedValidator{
		Pubkey:                    pubkey,
		PrivKey:                   privKey,
		Recorder:                  rec,
		Service:                   svc,
		DynamicSchedule:           dynamicSchedule,
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
		dynamicCollector:          dynamicCollector,
		voteAccount:               voteAccount,
		validatorStake:            validatorStake,
		lastSlot:                  blockStore.GetCurrentSlot(),
		leaderStartAtSlot:         NoSlot,
		collectedEntries:          make([]poh.Entry, 0),
		pendingValidTxs:           make([]*types.Transaction, 0, batchSize),
		epochStartTime:            time.Now(),
	}

	svc.OnEntry = v.handleEntry

	// Register validator stake with dynamic collector
	dynamicCollector.UpdateValidatorStake(pubkey, validatorStake)

	// Create vote account for this validator
	if err := dynamicCollector.CreateVoteAccount(voteAccount, pubkey, pubkey); err != nil {
		fmt.Printf("[ENHANCED_VALIDATOR] Failed to create vote account: %v\n", err)
	} else {
		fmt.Printf("[ENHANCED_VALIDATOR] Vote account created: %s for validator %s\n", voteAccount, pubkey)
	}

	return v
}

func (v *EnhancedValidator) onLeaderSlotStart(currentSlot uint64) {
	logx.Info("ENHANCED_LEADER", "onLeaderSlotStart", currentSlot)
	v.leaderStartAtSlot = currentSlot

	if currentSlot == 0 {
		return
	}

	prevSlot := currentSlot - 1
	ticker := time.NewTicker(v.leaderTimeoutLoopInterval)
	deadline := time.NewTimer(v.leaderTimeout)
	defer ticker.Stop()
	defer deadline.Stop()

	var seed blockstore.SlotBoundary

waitLoop:
	for {
		select {
		case <-ticker.C:
			if v.blockStore.HasCompleteBlock(prevSlot) {
				logx.Info("ENHANCED_LEADER", fmt.Sprintf("Found complete block for slot %d", prevSlot))
				seed, _ = v.blockStore.LastEntryInfoAtSlot(prevSlot)
				break waitLoop
			} else {
				logx.Info("ENHANCED_LEADER", fmt.Sprintf("No complete block for slot %d", prevSlot))
			}
		case <-deadline.C:
			logx.Info("ENHANCED_LEADER", fmt.Sprintf("Meet at deadline %d", prevSlot))
			seed = v.fastForwardTicks(prevSlot)
			break waitLoop
		case <-v.stopCh:
			return
		}
	}

	v.Recorder.Reset(seed.Hash, prevSlot)
	v.collectedEntries = make([]poh.Entry, 0, v.BatchSize)
	v.session = v.ledger.NewSession()
	v.lastSession = v.ledger.NewSession()
	v.pendingValidTxs = make([]*types.Transaction, 0, v.BatchSize)
}

func (v *EnhancedValidator) onLeaderSlotEnd() {
	logx.Info("ENHANCED_LEADER", "onLeaderSlotEnd")
	v.leaderStartAtSlot = NoSlot
	v.collectedEntries = make([]poh.Entry, 0, v.BatchSize)
	v.session = v.ledger.NewSession()
	v.lastSession = v.ledger.NewSession()
}

func (v *EnhancedValidator) fastForwardTicks(prevSlot uint64) blockstore.SlotBoundary {
	target := prevSlot * v.TicksPerSlot
	hash, _ := v.Recorder.FastForward(target)
	return blockstore.SlotBoundary{
		Slot: prevSlot,
		Hash: hash,
	}
}

// IsLeader checks if this validator is leader using dynamic scheduler
func (v *EnhancedValidator) IsLeader(slot uint64) bool {
	leader, has := v.DynamicSchedule.LeaderAt(slot)
	return has && leader == v.Pubkey
}

func (v *EnhancedValidator) IsFollower(slot uint64) bool {
	return !v.IsLeader(slot)
}

// GetCurrentEpochInfo returns information about current epoch
func (v *EnhancedValidator) GetCurrentEpochInfo(currentSlot uint64) *poh.EpochInfo {
	return v.DynamicSchedule.GetCurrentEpoch(currentSlot)
}

// handleEntry with enhanced dynamic features
func (v *EnhancedValidator) handleEntry(entries []poh.Entry) {
	currentSlot := v.Recorder.CurrentSlot()

	// Update blockstore current slot
	if currentSlot > v.blockStore.GetCurrentSlot() {
		// Note: We can't directly set current slot on blockstore interface
		// This would need to be implemented in the specific blockstore implementation
		// For now, it will be updated when blocks are added
	}

	// When slot advances, assemble block for lastSlot if we were leader
	// Skip slot 0 as it already has genesis block
	if currentSlot > v.lastSlot && v.lastSlot > 0 && v.IsLeader(v.lastSlot) {
		// Buffer entries
		v.collectedEntries = append(v.collectedEntries, entries...)

		// Retrieve previous block hash from blockStore
		lastEntry, _ := v.blockStore.LastEntryInfoAtSlot(v.lastSlot - 1)
		prevHash := lastEntry.Hash

		blk := block.AssembleBlock(
			v.lastSlot,
			prevHash,
			v.Pubkey,
			v.collectedEntries,
		)

		if err := v.ledger.VerifyBlock(blk); err != nil {
			logx.Error("ENHANCED_VALIDATOR", fmt.Sprintf("Sanity verify fail: %v", err))
			v.session = v.lastSession.CopyWithOverlayClone()
			return
		}

		blk.Sign(v.PrivKey)
		logx.Info("ENHANCED_VALIDATOR", fmt.Sprintf("Leader assembled block: slot=%d, entries=%d", v.lastSlot, len(v.collectedEntries)))

		// Persist then broadcast
		logx.Info("ENHANCED_VALIDATOR", fmt.Sprintf("Adding block pending: %d", blk.Slot))
		if err := v.blockStore.AddBlockPending(blk); err != nil {
			logx.Error("ENHANCED_VALIDATOR", fmt.Sprintf("Add block pending error: %v", err))
			return
		}
		if err := v.netClient.BroadcastBlock(context.Background(), blk); err != nil {
			logx.Error("ENHANCED_VALIDATOR", fmt.Sprintf("Failed to broadcast block: %v", err))
			return
		}

		// Create enhanced vote using dynamic collector
		vote := &consensus.Vote{
			Slot:      blk.Slot,
			BlockHash: blk.Hash,
			VoterID:   v.Pubkey,
		}
		vote.Sign(v.PrivKey)

		// Use dynamic voting with slot range and root slot
		slotRange := []uint64{blk.Slot}
		rootSlot := v.getRootSlot()

		fmt.Printf("[ENHANCED_LEADER] Adding dynamic vote %d to collector for self-vote\n", vote.Slot)
		if committed, needApply, err := v.dynamicCollector.AddDynamicVote(vote, v.voteAccount, slotRange, rootSlot); err != nil {
			fmt.Printf("[ENHANCED_LEADER] Add dynamic vote error: %v\n", err)
		} else {
			// Record vote in dynamic scheduler
			v.DynamicSchedule.RecordVote(v.Pubkey, vote.Slot)

			if committed && needApply {
				fmt.Printf("[ENHANCED_LEADER] slot %d committed with dynamic voting!\n", vote.Slot)
				block := v.blockStore.Block(vote.Slot)
				if block == nil {
					fmt.Printf("[ENHANCED_LEADER] Block not found for slot %d\n", vote.Slot)
				} else if err := v.ledger.ApplyBlock(block); err != nil {
					fmt.Printf("[ENHANCED_LEADER] Apply block error: %v\n", err)
				}
				if err := v.blockStore.MarkFinalized(vote.Slot); err != nil {
					fmt.Printf("[ENHANCED_LEADER] Mark block as finalized error: %v\n", err)
				}
				fmt.Printf("[ENHANCED_LEADER] slot %d finalized!\n", vote.Slot)
			}
		}

		// Broadcast vote
		fmt.Printf("[ENHANCED_LEADER] Broadcasted vote %d from %s\n", vote.Slot, v.Pubkey)
		if err := v.netClient.BroadcastVote(context.Background(), vote); err != nil {
			fmt.Printf("[ENHANCED_LEADER] Failed to broadcast vote: %v\n", err)
		}

		// Reset buffer
		v.collectedEntries = make([]poh.Entry, 0, v.BatchSize)
		v.lastSession = v.session.CopyWithOverlayClone()
	} else if v.IsLeader(currentSlot) {
		// Buffer entries only if leader of current slot
		v.collectedEntries = append(v.collectedEntries, entries...)
		fmt.Printf("Adding %d entries for slot %d\n", len(entries), currentSlot)
	}

	// Update lastSlot
	v.lastSlot = currentSlot
}

// getRootSlot returns the root slot for voting
func (v *EnhancedValidator) getRootSlot() uint64 {
	// This is a simplified implementation
	// In Solana, root slot is the highest slot that has been rooted (cannot be rolled back)
	finalizedSlot := v.blockStore.GetFinalizedSlot()
	if finalizedSlot > 32 {
		return finalizedSlot - 32
	}
	return 0
}

func (v *EnhancedValidator) peekPendingValidTxs(size int) []*types.Transaction {
	if len(v.pendingValidTxs) == 0 {
		return nil
	}
	if len(v.pendingValidTxs) < size {
		size = len(v.pendingValidTxs)
	}

	result := make([]*types.Transaction, size)
	copy(result, v.pendingValidTxs[:size])
	return result
}

func (v *EnhancedValidator) dropPendingValidTxs(size int) {
	if size >= len(v.pendingValidTxs) {
		v.pendingValidTxs = v.pendingValidTxs[:0]
		return
	}

	copy(v.pendingValidTxs, v.pendingValidTxs[size:])
	v.pendingValidTxs = v.pendingValidTxs[:len(v.pendingValidTxs)-size]
}

func (v *EnhancedValidator) Run() {
	v.stopCh = make(chan struct{})

	exception.SafeGoWithPanic("enhancedRoleMonitorLoop", func() {
		v.roleMonitorLoop()
	})

	exception.SafeGoWithPanic("enhancedLeaderBatchLoop", func() {
		v.leaderBatchLoop()
	})

	exception.SafeGoWithPanic("epochMonitorLoop", func() {
		v.epochMonitorLoop()
	})
}

func (v *EnhancedValidator) leaderBatchLoop() {
	batchTicker := time.NewTicker(v.leaderBatchLoopInterval)
	defer batchTicker.Stop()

	for {
		select {
		case <-v.stopCh:
			return
		case <-batchTicker.C:
			slot := v.Recorder.CurrentSlot()
			if !v.IsLeader(slot) {
				continue
			}

			fmt.Println("[ENHANCED_LEADER] Pulling batch")
			batch := v.Mempool.PullBatch(v.BatchSize)
			if len(batch) == 0 && len(v.pendingValidTxs) == 0 {
				fmt.Println("[ENHANCED_LEADER] No batch")
				continue
			}

			fmt.Println("[ENHANCED_LEADER] Filtering batch")
			valids, errs := v.session.FilterValid(batch)
			if len(errs) > 0 {
				fmt.Println("[ENHANCED_LEADER] Invalid transactions:", errs)
			}
			v.pendingValidTxs = append(v.pendingValidTxs, valids...)

			recordTxs := v.peekPendingValidTxs(v.BatchSize)
			if recordTxs == nil {
				fmt.Println("[ENHANCED_LEADER] No valid transactions")
				continue
			}

			fmt.Println("[ENHANCED_LEADER] Recording batch")
			entry, err := v.Recorder.RecordTxs(recordTxs)
			if err != nil {
				fmt.Println("[ENHANCED_LEADER] Record error:", err)
				continue
			}
			v.dropPendingValidTxs(len(recordTxs))
			fmt.Printf("[ENHANCED_LEADER] Recorded %d tx (slot=%d, entry=%x...)\n", len(recordTxs), slot, entry.Hash[:6])
		}
	}
}

func (v *EnhancedValidator) roleMonitorLoop() {
	ticker := time.NewTicker(v.roleMonitorLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			slot := v.Recorder.CurrentSlot()
			if v.IsLeader(slot) {
				if v.leaderStartAtSlot == NoSlot {
					v.onLeaderSlotStart(slot)
				}
			} else {
				if v.leaderStartAtSlot != NoSlot {
					v.onLeaderSlotEnd()
				}
			}
		}
	}
}

// epochMonitorLoop monitors epoch transitions and updates
func (v *EnhancedValidator) epochMonitorLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastEpoch uint64 = 0

	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			currentSlot := v.Recorder.CurrentSlot()
			epochInfo := v.GetCurrentEpochInfo(currentSlot)

			if epochInfo != nil && epochInfo.Epoch > lastEpoch {
				logx.Info("ENHANCED_VALIDATOR", fmt.Sprintf("Entered new epoch %d at slot %d", epochInfo.Epoch, currentSlot))
				v.epochStartTime = time.Now()

				// Update dynamic collector with new epoch info
				v.dynamicCollector.UpdateEpochInfo(&consensus.EpochInfo{
					Epoch:        epochInfo.Epoch,
					StartSlot:    epochInfo.StartSlot,
					EndSlot:      epochInfo.EndSlot,
					TotalStake:   epochInfo.TotalStake,
					ActiveStake:  epochInfo.TotalStake,
					StakeHistory: make(map[string]uint64),
				})

				lastEpoch = epochInfo.Epoch
			}
		}
	}
}

// UpdateStake updates this validator's stake amount
func (v *EnhancedValidator) UpdateStake(newStake uint64) {
	v.validatorStake = newStake
	v.DynamicSchedule.UpdateValidatorStake(v.Pubkey, newStake)
	v.dynamicCollector.UpdateValidatorStake(v.Pubkey, newStake)
}

// GetValidatorInfo returns information about this validator
func (v *EnhancedValidator) GetValidatorInfo() map[string]interface{} {
	currentSlot := v.Recorder.CurrentSlot()
	epochInfo := v.GetCurrentEpochInfo(currentSlot)

	info := make(map[string]interface{})
	info["pubkey"] = v.Pubkey
	info["vote_account"] = v.voteAccount
	info["stake"] = v.validatorStake
	info["is_leader"] = v.IsLeader(currentSlot)
	info["current_slot"] = currentSlot
	info["last_slot"] = v.lastSlot

	if epochInfo != nil {
		info["epoch"] = epochInfo.Epoch
		info["epoch_start_slot"] = epochInfo.StartSlot
		info["epoch_end_slot"] = epochInfo.EndSlot
		info["total_validators"] = len(epochInfo.Validators)
	}

	return info
}
