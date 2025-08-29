package validator

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/mezonai/mmn/snapshot"
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

// Validator encapsulates leader/follower behavior.
type Validator struct {
	Pubkey       string
	PrivKey      ed25519.PrivateKey
	Recorder     *poh.PohRecorder
	Service      *poh.PohService
	Schedule     *poh.LeaderSchedule
	Mempool      *mempool.Mempool
	TicksPerSlot uint64

	// Configurable parameters
	leaderBatchLoopInterval   time.Duration
	roleMonitorLoopInterval   time.Duration
	leaderTimeout             time.Duration
	leaderTimeoutLoopInterval time.Duration
	BatchSize                 int

	netClient   interfaces.Broadcaster
	blockStore  store.BlockStore
	ledger      *ledger.Ledger
	session     *ledger.Session
	lastSession *ledger.Session
	collector   *consensus.Collector
	// Slot & entry buffer
	lastSlot          uint64
	leaderStartAtSlot uint64
	collectedEntries  []poh.Entry
	pendingValidTxs   []*transaction.Transaction
	stopCh            chan struct{}
}

// NewValidator constructs a Validator with dependencies, including blockStore.
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
		lastSlot:                  1,
		leaderStartAtSlot:         NoSlot,
		collectedEntries:          make([]poh.Entry, 0),
		collector:                 collector,
		pendingValidTxs:           make([]*transaction.Transaction, 0, batchSize),
	}
	svc.OnEntry = v.handleEntry
	return v
}

// SetLeaderSchedule allows updating the leader schedule at runtime (e.g., from snapshot)
func (v *Validator) SetLeaderSchedule(schedule *poh.LeaderSchedule) {
	v.Schedule = schedule
	if v.Recorder != nil {
		v.Recorder.SetLeaderSchedule(schedule)
	}
}

func (v *Validator) onLeaderSlotStart(currentSlot uint64) {
	logx.Info("LEADER", "onLeaderSlotStart", currentSlot)
	v.leaderStartAtSlot = currentSlot
	if currentSlot == 0 {
		return
	}
	prevSlot := currentSlot - 1

	// Check if we have the previous block before proceeding
	if !v.blockStore.HasCompleteBlock(prevSlot) {
		logx.Info("LEADER", fmt.Sprintf("Previous block for slot %d not available yet, waiting", prevSlot))
		return
	}

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
	leader, has := v.Schedule.LeaderAt(slot)
	return has && leader == v.Pubkey
}

func (v *Validator) IsFollower(slot uint64) bool {
	return !v.IsLeader(slot)
}

// handleEntry buffers entries and assembles a block at slot boundary.
func (v *Validator) handleEntry(entries []poh.Entry) {
	// Check if network is ready before processing
	if v.netClient != nil {
		if ready, ok := v.netClient.(interface{ IsNodeReady() bool }); ok && !ready.IsNodeReady() {
			return // Skip processing if network not ready
		}
	}

	currentSlot := v.Recorder.CurrentSlot()

	// When slot advances, assemble block for lastSlot if we were leader
	if currentSlot > v.lastSlot && v.IsLeader(v.lastSlot) {

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
			v.session = v.lastSession.CopyWithOverlayClone()
			return
		}

		blk.Sign(v.PrivKey)
		logx.Info("VALIDATOR", fmt.Sprintf("Leader assembled block: slot=%d, entries=%d", v.lastSlot, len(v.collectedEntries)))

		// Check if block already exists before trying to add it
		if v.blockStore.HasCompleteBlock(v.lastSlot) {
			logx.Info("VALIDATOR", fmt.Sprintf("Block at slot %d already exists, skipping", v.lastSlot))
			// Reset buffer and continue
			v.collectedEntries = make([]poh.Entry, 0, v.BatchSize)
			v.lastSession = v.session.CopyWithOverlayClone()
			return
		}

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

		writeSnapshotIfDue(v.ledger, currentSlot)

		// Self-vote
		vote := &consensus.Vote{
			Slot:      blk.Slot,
			BlockHash: blk.Hash,
			VoterID:   v.Pubkey,
		}
		vote.Sign(v.PrivKey)
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

		// Broadcast vote
		fmt.Printf("[LEADER] Broadcasted vote %d to %s\n", vote.Slot, v.Pubkey)
		if err := v.netClient.BroadcastVote(context.Background(), vote); err != nil {
			fmt.Printf("[LEADER] Failed to broadcast vote: %v\n", err)
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

	// Wait for network to be ready before starting validator loops
	logx.Info("VALIDATOR", "Waiting for network to be ready before starting...")
	for {
		if v.netClient != nil {
			if ready, ok := v.netClient.(interface{ IsNodeReady() bool }); ok && ready.IsNodeReady() {
				break
			}
		}
	}
	logx.Info("VALIDATOR", "Network is ready, starting validator loops")

	exception.SafeGoWithPanic("roleMonitorLoop", func() {
		v.roleMonitorLoop()
	})

	exception.SafeGoWithPanic("leaderBatchLoop", func() {
		v.leaderBatchLoop()
	})
}

func (v *Validator) leaderBatchLoop() {
	batchTicker := time.NewTicker(v.leaderBatchLoopInterval)
	defer batchTicker.Stop()
	for {
		select {
		case <-v.stopCh:
			return
		case <-batchTicker.C:
			// Check if network is ready before processing
			if v.netClient != nil {
				if ready, ok := v.netClient.(interface{ IsNodeReady() bool }); ok && !ready.IsNodeReady() {
					continue // Skip processing if network not ready
				}
			}

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
					logx.Info("onLeaderSlotStart", slot)
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

func writeSnapshotIfDue(ld *ledger.Ledger, slot uint64) {
	if slot%p2p.RangeForSnapshot != 0 { // adjust interval as needed
		return
	}
	accountStore := ld.GetAccountStore()
	if accountStore == nil {
		return
	}
	dbProvider := accountStore.GetDatabaseProvider()
	if dbProvider == nil {
		return
	}
	bankHash, err := snapshot.ComputeFullBankHash(dbProvider)
	if err != nil {
		logx.Error("SNAPSHOT", fmt.Sprintf("BankHash compute failed at slot %d: %v", slot, err))
		return
	}
	dir := "/data/snapshots"
	// Write snapshot and cleanup old ones, keep only the latest
	saved, err := snapshot.WriteSnapshotAndCleanup(dir, dbProvider, slot, bankHash, nil)
	if err != nil {
		logx.Error("SNAPSHOT", fmt.Sprintf("Failed to write snapshot at slot %d: %v", slot, err))
		return
	}

	logx.Info("SNAPSHOT", fmt.Sprintf("Created snapshot: %s (slot %d)", saved, slot))
}
