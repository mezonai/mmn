package validator

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/mezonai/mmn/store"

	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/utils"

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

	netClient  interfaces.Broadcaster
	blockStore store.BlockStore
	ledger     *ledger.Ledger
	collector  *consensus.Collector
	// Slot & entry buffer
	lastSlot          uint64
	leaderStartAtSlot uint64
	collectedEntries  []poh.Entry
	pendingTxs        []*transaction.Transaction
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
		lastSlot:                  0,
		leaderStartAtSlot:         NoSlot,
		collectedEntries:          make([]poh.Entry, 0),
		collector:                 collector,
		pendingTxs:                make([]*transaction.Transaction, 0, batchSize),
	}
	svc.OnEntry = v.handleEntry
	p2pClient.OnSyncPohFromLeader = v.handleResetPohFromLeader
	return v
}

func (v *Validator) onLeaderSlotStart(currentSlot uint64) {
	logx.Info("LEADER", "onLeaderSlotStart", currentSlot)
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
			lastSeenSlot := v.blockStore.GetLatestSlot()
			lastSeenEntry, _ := v.blockStore.LastEntryInfoAtSlot(lastSeenSlot)
			seed = v.fastForwardTicks(lastSeenEntry.Hash, lastSeenEntry.Slot, prevSlot)
			break waitLoop
		case <-v.stopCh:
			return
		}
	}

	v.Recorder.Reset(seed.Hash, prevSlot)
	v.collectedEntries = make([]poh.Entry, 0, v.BatchSize)
	v.pendingTxs = make([]*transaction.Transaction, 0, v.BatchSize)
	v.leaderStartAtSlot = currentSlot
	logx.Info("LEADER", fmt.Sprintf("Leader ready to start at slot: %d", currentSlot))
}

func (v *Validator) onLeaderSlotEnd() {
	logx.Info("LEADER", "onLeaderSlotEnd")
	// TODO: temporary fix bug race condition
	// v.collectedEntries = make([]poh.Entry, 0, v.BatchSize)
	v.leaderStartAtSlot = NoSlot
}

func (v *Validator) fastForwardTicks(seenHash [32]byte, fromSlot uint64, toSlot uint64) store.SlotBoundary {
	hash := v.Recorder.FastForward(seenHash, fromSlot, toSlot)
	return store.SlotBoundary{
		Slot: toSlot,
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

func (v *Validator) ReadyToStart(slot uint64) bool {
	return v.IsLeader(slot) && v.leaderStartAtSlot != NoSlot
}

// handleEntry buffers entries and assembles a block at slot boundary.
func (v *Validator) handleEntry(entries []poh.Entry) {
	currentSlot := v.Recorder.CurrentSlot()

	// When slot advances, assemble block for lastSlot if we were leader
	if currentSlot > v.lastSlot && v.IsLeader(v.lastSlot) {

		// Buffer entries
		v.collectedEntries = append(v.collectedEntries, entries...)

		// Retrieve previous hash from recorder
		prevHash := v.Recorder.GetSlotHash(v.lastSlot - 1)
		logx.Info("VALIDATOR", fmt.Sprintf("Previous hash for slot %d %x", v.lastSlot-1, prevHash))

		blk := block.AssembleBlock(
			v.lastSlot,
			prevHash,
			v.Pubkey,
			v.collectedEntries,
		)

		blk.Sign(v.PrivKey)
		logx.Info("VALIDATOR", fmt.Sprintf("Leader assembled block: slot=%d, entries=%d", v.lastSlot, len(v.collectedEntries)))

		// Reset buffer
		v.collectedEntries = make([]poh.Entry, 0, v.BatchSize)

		if err := v.netClient.BroadcastBlock(context.Background(), blk); err != nil {
			logx.Error("VALIDATOR", fmt.Sprintf("Failed to broadcast block: %v", err))
		}
	} else if v.IsLeader(currentSlot) {
		// Buffer entries only if leader of current slot
		v.collectedEntries = append(v.collectedEntries, entries...)
		logx.Info("VALIDATOR", fmt.Sprintf("Adding %d entries for slot %d", len(entries), currentSlot))
	}

	// Update lastSlot
	v.lastSlot = currentSlot
}

func (v *Validator) handleResetPohFromLeader(seedHash [32]byte, slot uint64) error {
	logx.Info("VALIDATOR", fmt.Sprintf("Received latest slot %d", slot))
	currentSlot := v.Recorder.CurrentSlot()
	if v.IsFollower(currentSlot) {
		logx.Info("VALIDATOR", fmt.Sprintf("Follower received latest slot %d", slot))
		v.Recorder.Reset(seedHash, slot)
	}
	return nil
}

func (v *Validator) peekPendingTxs(size int) []*transaction.Transaction {
	if len(v.pendingTxs) == 0 {
		return nil
	}
	if len(v.pendingTxs) < size {
		size = len(v.pendingTxs)
	}

	result := make([]*transaction.Transaction, size)
	copy(result, v.pendingTxs[:size])

	return result
}

func (v *Validator) dropPendingTxs(size int) {
	if size >= len(v.pendingTxs) {
		v.pendingTxs = v.pendingTxs[:0]
		return
	}

	copy(v.pendingTxs, v.pendingTxs[size:])
	v.pendingTxs = v.pendingTxs[:len(v.pendingTxs)-size]
}

func (v *Validator) Run() {
	v.stopCh = make(chan struct{})

	exception.SafeGoWithPanic("roleMonitorLoop", func() {
		v.roleMonitorLoop()
	})

	exception.SafeGoWithPanic("leaderBatchLoop", func() {
		v.leaderBatchLoop()
	})

	exception.SafeGoWithPanic("mempoolCleanupLoop", func() {
		v.mempoolCleanupLoop()
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
			slot := v.Recorder.CurrentSlot()
			if !v.IsLeader(slot) {
				continue
			}

			if !v.ReadyToStart(slot) {
				logx.Warn("LEADER", fmt.Sprintf("Leader batch loop: leader has not ready to start for slot %d", slot))
				continue
			}

			logx.Info("LEADER", fmt.Sprintf("Pulling batch for slot %d", slot))
			batch := v.Mempool.PullBatch(v.BatchSize)
			if len(batch) == 0 && len(v.pendingTxs) == 0 {
				logx.Info("LEADER", fmt.Sprintf("No batch for slot %d", slot))
				continue
			}

			for _, r := range batch {
				tx, err := utils.ParseTx(r)
				if err != nil {
					logx.Error("LEADER", fmt.Sprintf("Failed to parse transaction: %v", err))
					continue
				}
				v.pendingTxs = append(v.pendingTxs, tx)
			}

			recordTxs := v.peekPendingTxs(v.BatchSize)
			if recordTxs == nil {
				logx.Info("LEADER", fmt.Sprintf("No valid transactions for slot %d", slot))
				continue
			}
			logx.Info("LEADER", fmt.Sprintf("Recording batch for slot %d", slot))
			entry, err := v.Recorder.RecordTxs(recordTxs)
			if err != nil {
				logx.Warn("LEADER", fmt.Sprintf("Record error: %v", err))
				continue
			}
			v.dropPendingTxs(len(recordTxs))
			logx.Info("LEADER", fmt.Sprintf("Recorded %d tx (slot=%d, entry=%x...)", len(recordTxs), slot, entry.Hash[:6]))
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

func (v *Validator) mempoolCleanupLoop() {
	cleanupTicker := time.NewTicker(1 * time.Minute)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-v.stopCh:
			return
		case <-cleanupTicker.C:
			logx.Info("VALIDATOR", "Running periodic mempool cleanup")
			v.Mempool.PeriodicCleanup()
		}
	}
}
