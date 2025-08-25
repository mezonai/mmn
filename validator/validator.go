package validator

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/mezonai/mmn/store"

	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/transaction"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/p2p"
	"github.com/mezonai/mmn/poh"
)

const NoSlot = ^uint64(0)

// EpochInfo holds epoch-related information
type EpochInfo struct {
	Epoch      uint64
	StartSlot  uint64
	EndSlot    uint64
	TotalStake uint64
	Validators map[string]*poh.Validator
}

// Validator encapsulates leader/follower behavior with support for both static and dynamic scheduling.
type Validator struct {
	Pubkey          string
	PrivKey         ed25519.PrivateKey
	Recorder        *poh.PohRecorder
	Service         *poh.PohService
	Schedule        *poh.LeaderSchedule        // For static scheduling (can be nil)
	DynamicSchedule *poh.DynamicLeaderSchedule // For dynamic scheduling (can be nil)
	Mempool         *mempool.Mempool
	TicksPerSlot    uint64

	// Configurable parameters
	leaderBatchLoopInterval   time.Duration
	roleMonitorLoopInterval   time.Duration
	leaderTimeout             time.Duration
	leaderTimeoutLoopInterval time.Duration
	BatchSize                 int

	netClient        interfaces.Broadcaster
	blockStore       store.BlockStore
	ledger           *ledger.Ledger
	session          *ledger.Session
	lastSession      *ledger.Session
	collector        *consensus.Collector        // For static consensus
	dynamicCollector *consensus.DynamicCollector // For dynamic consensus

	// Enhanced PoS features (only used when dynamic scheduling is enabled)
	voteAccount    string    // This validator's vote account
	validatorStake uint64    // Current stake amount
	epochStartTime time.Time // When current epoch started
	useDynamic     bool      // Whether to use dynamic scheduling

	// Slot & entry buffer
	lastSlot          uint64
	leaderStartAtSlot uint64
	collectedEntries  []poh.Entry
	pendingValidTxs   []*transaction.Transaction
	stopCh            chan struct{}
}

// NewValidator constructs a Validator with static scheduling
func NewValidator(
	pubkey string,
	privKey ed25519.PrivateKey,
	rec *poh.PohRecorder,
	svc *poh.PohService,
	schedule *poh.LeaderSchedule,
	mempool *mempool.Mempool,
	ticksPerSlot uint64,
	leaderBatchLoopInterval time.Duration,
	roleMonitorLoopInterval time.Duration,
	leaderTimeout time.Duration,
	leaderTimeoutLoopInterval time.Duration,
	batchSize int,
	p2pClient *p2p.Libp2pNetwork,
	blockStore store.BlockStore,
	ledger *ledger.Ledger,
	collector *consensus.Collector,
) *Validator {
	v := &Validator{
		Pubkey:                    pubkey,
		PrivKey:                   privKey,
		Recorder:                  rec,
		Service:                   svc,
		Schedule:                  schedule,
		DynamicSchedule:           nil,
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
		collector:                 collector,
		dynamicCollector:          nil,
		useDynamic:                false,
		lastSlot:                  0,
		leaderStartAtSlot:         NoSlot,
		collectedEntries:          make([]poh.Entry, 0),
		pendingValidTxs:           make([]*transaction.Transaction, 0, batchSize),
	}
	svc.OnEntry = v.handleEntry
	return v
}

// NewValidatorWithDynamic constructs a Validator with dynamic PoS scheduling
func NewValidatorWithDynamic(
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
	blockStore store.BlockStore,
	ledger *ledger.Ledger,
	dynamicCollector *consensus.DynamicCollector,
	voteAccount string,
	validatorStake uint64,
) *Validator {
	v := &Validator{
		Pubkey:                    pubkey,
		PrivKey:                   privKey,
		Recorder:                  rec,
		Service:                   svc,
		Schedule:                  nil,
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
		collector:                 nil,
		dynamicCollector:          dynamicCollector,
		voteAccount:               voteAccount,
		validatorStake:            validatorStake,
		useDynamic:                true,
		epochStartTime:            time.Now(),
		lastSlot:                  0,
		leaderStartAtSlot:         NoSlot,
		collectedEntries:          make([]poh.Entry, 0),
		pendingValidTxs:           make([]*transaction.Transaction, 0, batchSize),
	}

	svc.OnEntry = v.handleEntry

	// Register validator stake with dynamic collector
	dynamicCollector.UpdateValidatorStake(pubkey, validatorStake)

	// Create vote account for this validator
	if err := dynamicCollector.CreateVoteAccount(voteAccount, pubkey, pubkey); err != nil {
		fmt.Printf("[VALIDATOR] Failed to create vote account: %v\n", err)
	} else {
		fmt.Printf("[VALIDATOR] Vote account created: %s for validator %s\n", voteAccount, pubkey)
	}

	return v
}

// GetCurrentEpochInfo returns information about the current epoch (only for dynamic scheduling)
func (v *Validator) GetCurrentEpochInfo(slot uint64) *poh.EpochInfo {
	if !v.useDynamic || v.DynamicSchedule == nil {
		return nil
	}
	return v.DynamicSchedule.GetCurrentEpoch(slot)
}

// GetCurrentLeaderRange returns the leader range containing the current slot
func (v *Validator) GetCurrentLeaderRange(slot uint64) (*poh.LeaderSlotRange, bool) {
	if !v.useDynamic || v.DynamicSchedule == nil {
		return nil, false
	}
	return v.DynamicSchedule.GetLeaderRange(slot)
}

// GetAllEpochRanges returns all leader ranges for the current epoch
func (v *Validator) GetAllEpochRanges(slot uint64) []poh.LeaderSlotRange {
	if !v.useDynamic || v.DynamicSchedule == nil {
		return []poh.LeaderSlotRange{}
	}
	return v.DynamicSchedule.GetCurrentEpochRanges(slot)
}

func (v *Validator) onLeaderSlotStart(currentSlot uint64) {
	logx.Info("LEADER", "onLeaderSlotStart", currentSlot)

	// Log leader range information for dynamic scheduling
	if v.useDynamic && v.DynamicSchedule != nil {
		if leaderRange, found := v.GetCurrentLeaderRange(currentSlot); found {
			logx.Info("LEADER", fmt.Sprintf("Leader range: slots %d-%d (total %d slots)",
				leaderRange.StartSlot, leaderRange.EndSlot, leaderRange.SlotCount))
		}
	}

	v.leaderStartAtSlot = currentSlot
	if currentSlot == 0 {
		return
	}
	prevSlot := currentSlot - 1
	ticker := time.NewTicker(v.leaderTimeoutLoopInterval)
	deadline := time.NewTimer(v.leaderTimeout)
	defer ticker.Stop()
	defer deadline.Stop()

	var seed store.SlotBoundary

waitLoop:
	for {
		select {
		case <-ticker.C:
			if v.blockStore.HasCompleteBlock(prevSlot) {
				logx.Info("LEADER", fmt.Sprintf("Found complete block for slot %d", prevSlot))
				seed, _ = v.blockStore.LastEntryInfoAtSlot(prevSlot)
				break waitLoop
			} else {
				logx.Info("LEADER", fmt.Sprintf("No complete block for slot %d", prevSlot))
			}
		case <-deadline.C:
			logx.Info("LEADER", fmt.Sprintf("Meet at deadline %d", prevSlot))
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
	v.pendingValidTxs = make([]*transaction.Transaction, 0, v.BatchSize)
}

func (v *Validator) onLeaderSlotEnd() {
	logx.Info("LEADER", "onLeaderSlotEnd")
	v.leaderStartAtSlot = NoSlot
	v.collectedEntries = make([]poh.Entry, 0, v.BatchSize)
	v.session = v.ledger.NewSession()
	v.lastSession = v.ledger.NewSession()
}

func (v *Validator) fastForwardTicks(prevSlot uint64) store.SlotBoundary {
	target := prevSlot * v.TicksPerSlot
	hash, _ := v.Recorder.FastForward(target)
	return store.SlotBoundary{
		Slot: prevSlot,
		Hash: hash,
	}
}

// IsLeader checks if this validator is leader for given slot.
func (v *Validator) IsLeader(slot uint64) bool {
	if v.useDynamic && v.DynamicSchedule != nil {
		leader, found := v.DynamicSchedule.LeaderAt(slot)
		return found && leader == v.Pubkey
	}
	if v.Schedule != nil {
		leader, found := v.Schedule.LeaderAt(slot)
		return found && leader == v.Pubkey
	}
	return false
}

func (v *Validator) IsFollower(slot uint64) bool {
	return !v.IsLeader(slot)
}

// handleEntry buffers entries and assembles a block at slot boundary.
func (v *Validator) handleEntry(entries []poh.Entry) {
	currentSlot := v.Recorder.CurrentSlot()

	// When slot advances, assemble block for lastSlot if we were leader
	// Skip slot 0 as it already has genesis block
	if currentSlot > v.lastSlot && v.lastSlot > 0 && v.IsLeader(v.lastSlot) {
		// Check if block already exists for this slot to avoid duplicates
		if v.blockStore.Block(v.lastSlot) != nil {
			logx.Info("VALIDATOR", fmt.Sprintf("Block already exists for slot %d, skipping", v.lastSlot))
		} else {
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
				logx.Error("VALIDATOR", fmt.Sprintf("Sanity verify fail: %v", err))
				// Reset session to fresh state based on current ledger state
				v.session = v.ledger.NewSession()
				v.lastSession = v.ledger.NewSession()
				return
			}

			blk.Sign(v.PrivKey)
			logx.Info("VALIDATOR", fmt.Sprintf("Leader assembled block: slot=%d, entries=%d", v.lastSlot, len(v.collectedEntries)))

			// Persist then broadcast
			logx.Info("VALIDATOR", fmt.Sprintf("Adding block pending: %d", blk.Slot))
			if err := v.blockStore.AddBlockPending(blk); err != nil {
				logx.Error("VALIDATOR", fmt.Sprintf("Add block pending error: %v", err))
				return
			}
			if err := v.netClient.BroadcastBlock(context.Background(), blk); err != nil {
				logx.Error("VALIDATOR", fmt.Sprintf("Failed to broadcast block: %v", err))
				return
			}

			// Create and handle vote based on mode
			vote := &consensus.Vote{
				Slot:      blk.Slot,
				BlockHash: blk.Hash,
				VoterID:   v.Pubkey,
			}
			vote.Sign(v.PrivKey)

			if v.useDynamic && v.dynamicCollector != nil {
				// Dynamic PoS voting
				slotRange := []uint64{blk.Slot}
				rootSlot := v.getRootSlot()

				fmt.Printf("[LEADER] Adding dynamic vote %d to collector for self-vote\n", vote.Slot)
				if committed, needApply, err := v.dynamicCollector.AddDynamicVote(vote, v.voteAccount, slotRange, rootSlot); err != nil {
					fmt.Printf("[LEADER] Add dynamic vote error: %v\n", err)
				} else {
					// Record vote in dynamic scheduler
					v.DynamicSchedule.RecordVote(v.Pubkey, vote.Slot)

					if committed && needApply {
						fmt.Printf("[LEADER] slot %d committed with dynamic voting!\n", vote.Slot)
						block := v.blockStore.Block(vote.Slot)
						if block == nil {
							fmt.Printf("[LEADER] Block not found for slot %d\n", vote.Slot)
						} else if err := v.ledger.ApplyBlock(block); err != nil {
							fmt.Printf("[LEADER] Apply block error: %v\n", err)
						}
						if err := v.blockStore.MarkFinalized(vote.Slot); err != nil {
							fmt.Printf("[LEADER] Mark block as finalized error: %v\n", err)
						}
						fmt.Printf("[LEADER] slot %d finalized!\n", vote.Slot)
					}
				}
			} else if v.collector != nil {
				// Static consensus voting
				fmt.Printf("[LEADER] Adding vote %d to collector for self-vote\n", vote.Slot)
				if committed, needApply, err := v.collector.AddVote(vote); err != nil {
					fmt.Printf("[LEADER] Add vote error: %v\n", err)
				} else if committed && needApply {
					fmt.Printf("[LEADER] slot %d committed, processing apply block! votes=%d\n", vote.Slot, len(v.collector.VotesForSlot(vote.Slot)))
					block := v.blockStore.Block(vote.Slot)
					if block == nil {
						fmt.Printf("[LEADER] Block not found for slot %d\n", vote.Slot)
					} else if err := v.ledger.ApplyBlock(block); err != nil {
						fmt.Printf("[LEADER] Apply block error: %v\n", err)
					}
					if err := v.blockStore.MarkFinalized(vote.Slot); err != nil {
						fmt.Printf("[LEADER] Mark block as finalized error: %v\n", err)
					}
					fmt.Printf("[LEADER] slot %d finalized!\n", vote.Slot)
				}
			}

			// Broadcast vote
			fmt.Printf("[LEADER] Broadcasted vote %d from %s\n", vote.Slot, v.Pubkey)
			if err := v.netClient.BroadcastVote(context.Background(), vote); err != nil {
				fmt.Printf("[LEADER] Failed to broadcast vote: %v\n", err)
			}

			// Reset buffer
			v.collectedEntries = make([]poh.Entry, 0, v.BatchSize)
			v.lastSession = v.session.CopyWithOverlayClone()
		}
	} else if v.IsLeader(currentSlot) {
		// Buffer entries only if leader of current slot
		v.collectedEntries = append(v.collectedEntries, entries...)
		fmt.Printf("Adding %d entries for slot %d\n", len(entries), currentSlot)
	}

	// Update lastSlot
	v.lastSlot = currentSlot
}

// getRootSlot returns the root slot for voting (only for dynamic mode)
func (v *Validator) getRootSlot() uint64 {
	// This is a simplified implementation
	// In Solana, root slot is the highest slot that has been rooted (cannot be rolled back)
	if v.blockStore.GetLatestSlot() > 32 {
		return v.blockStore.GetLatestSlot() - 32
	}
	return 0
}

func (v *Validator) peekPendingValidTxs(size int) []*transaction.Transaction {
	if len(v.pendingValidTxs) == 0 {
		return nil
	}
	if len(v.pendingValidTxs) < size {
		size = len(v.pendingValidTxs)
	}

	result := make([]*transaction.Transaction, size)
	copy(result, v.pendingValidTxs[:size])

	return result
}

func (v *Validator) dropPendingValidTxs(size int) {
	if size >= len(v.pendingValidTxs) {
		v.pendingValidTxs = v.pendingValidTxs[:0]
		return
	}

	copy(v.pendingValidTxs, v.pendingValidTxs[size:])
	v.pendingValidTxs = v.pendingValidTxs[:len(v.pendingValidTxs)-size]
}

func (v *Validator) Run() {
	v.stopCh = make(chan struct{})

	exception.SafeGoWithPanic("roleMonitorLoop", func() {
		v.roleMonitorLoop()
	})

	exception.SafeGoWithPanic("leaderBatchLoop", func() {
		v.leaderBatchLoop()
	})

	// Add epoch monitoring loop for dynamic mode
	if v.useDynamic {
		exception.SafeGoWithPanic("epochMonitorLoop", func() {
			v.epochMonitorLoop()
		})
	}
}

func (v *Validator) leaderBatchLoop() {
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

			fmt.Println("[LEADER] Pulling batch")
			batch := v.Mempool.PullBatch(v.BatchSize)
			if len(batch) == 0 && len(v.pendingValidTxs) == 0 {
				fmt.Println("[LEADER] No batch")
				continue
			}

			fmt.Println("[LEADER] Filtering batch")
			valids, errs := v.session.FilterValid(batch)
			if len(errs) > 0 {
				fmt.Println("[LEADER] Invalid transactions:", errs)
			}
			v.pendingValidTxs = append(v.pendingValidTxs, valids...)

			recordTxs := v.peekPendingValidTxs(v.BatchSize)
			if recordTxs == nil {
				fmt.Println("[LEADER] No valid transactions")
				continue
			}
			fmt.Println("[LEADER] Recording batch")
			entry, err := v.Recorder.RecordTxs(recordTxs)
			if err != nil {
				fmt.Println("[LEADER] Record error:", err)
				continue
			}
			v.dropPendingValidTxs(len(recordTxs))
			fmt.Printf("[LEADER] Recorded %d tx (slot=%d, entry=%x...)\n", len(recordTxs), slot, entry.Hash[:6])
		}
	}
}

func (v *Validator) roleMonitorLoop() {
	ticker := time.NewTicker(v.roleMonitorLoopInterval)
	defer ticker.Stop()

	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			slot := v.Recorder.CurrentSlot()
			if v.IsLeader(slot) {
				// fmt.Println("Switched to LEADER for slot", slot, "at", time.Now().Format(time.RFC3339))
				if v.leaderStartAtSlot == NoSlot {
					v.onLeaderSlotStart(slot)
				}
			} else {
				// fmt.Println("Switched to FOLLOWER for slot", slot, "at", time.Now().Format(time.RFC3339))
				if v.leaderStartAtSlot != NoSlot {
					v.onLeaderSlotEnd()
				}
			}
		}
	}
}

// epochMonitorLoop monitors epoch transitions and updates (for dynamic mode only)
func (v *Validator) epochMonitorLoop() {
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
				logx.Info("VALIDATOR", fmt.Sprintf("Entered new epoch %d at slot %d", epochInfo.Epoch, currentSlot))
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

// UpdateStake updates this validator's stake amount (for dynamic mode only)
func (v *Validator) UpdateStake(newStake uint64) {
	if !v.useDynamic {
		return
	}

	v.validatorStake = newStake
	if v.DynamicSchedule != nil {
		v.DynamicSchedule.UpdateValidatorStake(v.Pubkey, newStake)
	}
	if v.dynamicCollector != nil {
		v.dynamicCollector.UpdateValidatorStake(v.Pubkey, newStake)
	}
}

// GetValidatorInfo returns information about this validator
func (v *Validator) GetValidatorInfo() map[string]interface{} {
	currentSlot := v.Recorder.CurrentSlot()
	info := make(map[string]interface{})

	info["pubkey"] = v.Pubkey
	info["is_leader"] = v.IsLeader(currentSlot)
	info["current_slot"] = currentSlot
	info["last_slot"] = v.lastSlot
	info["use_dynamic"] = v.useDynamic

	if v.useDynamic {
		info["vote_account"] = v.voteAccount
		info["stake"] = v.validatorStake

		epochInfo := v.GetCurrentEpochInfo(currentSlot)
		if epochInfo != nil {
			info["epoch"] = epochInfo.Epoch
			info["epoch_start_slot"] = epochInfo.StartSlot
			info["epoch_end_slot"] = epochInfo.EndSlot
			info["total_validators"] = len(epochInfo.Validators)
		}
	}

	return info
}
