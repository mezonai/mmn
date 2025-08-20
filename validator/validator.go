package validator

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mezonai/mmn/types"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/blockstore"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
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
	blockStore  blockstore.Store
	ledger      *ledger.Ledger
	session     *ledger.Session
	lastSession *ledger.Session
	collector   *consensus.Collector

	// Shredding support
	shredder *Shredder

	// Entry buffer for shredding
	entryBuffer   [][]byte // Serialized entries ready for shredding
	bufferSize    int      // Current buffer size in bytes
	maxBufferSize int      // Threshold to trigger shred broadcast

	// Slot & entry buffer
	lastSlot          uint64
	leaderStartAtSlot uint64
	collectedEntries  []poh.Entry
	pendingValidTxs   []*types.Transaction
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
	blockStore blockstore.Store,
	ledger *ledger.Ledger,
	collector *consensus.Collector,
	shredderCfg ShredderConfig,
	shredder *Shredder,
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
		lastSlot:                  0,
		leaderStartAtSlot:         NoSlot,
		collectedEntries:          make([]poh.Entry, 0),
		collector:                 collector,
		shredder:                  shredder,
		entryBuffer:               make([][]byte, 0),
		bufferSize:                0,
		maxBufferSize:             shredderCfg.MaxPayload * shredderCfg.K,
		pendingValidTxs:           make([]*types.Transaction, 0, batchSize),
	}
	svc.OnEntry = v.handleEntry
	return v
}

// serializeEntry converts a PoH entry to bytes for shredding
func (v *Validator) serializeEntry(entry poh.Entry) ([]byte, error) {
	return json.Marshal(entry)
}

// tryBroadcastShreds checks if buffer threshold is reached and broadcasts shreds if so
func (v *Validator) tryBroadcastShreds(slot uint64) error {
	logx.Info("LEADER", "tryBroadcastShreds", "bufferSize", v.bufferSize, "maxBufferSize", v.maxBufferSize)
	if v.bufferSize < v.maxBufferSize {
		return nil // Not enough data yet
	}

	// Create signing function
	signer := NewSigner(v.PrivKey)

	// Generate shreds from buffered entries
	dataShreds, codingShreds, err := v.shredder.MakeShreds(
		slot,
		v.entryBuffer,
		signer,
		1, // version
		0, // not end_of_slot yet
	)
	if err != nil {
		logx.Error("LEADER", "tryBroadcastShreds", "error", err)
		return err
	}

	logx.Info("LEADER", "tryBroadcastShreds", "Broadcasting", len(dataShreds),
		"data +", len(codingShreds),
		"coding shreds for slot", slot,
		"(buffer size:", v.bufferSize, "bytes)")

	// Broadcast data shreds first (more important)
	for i, shred := range dataShreds {
		if err := v.netClient.BroadcastShred(context.Background(), &shred); err != nil {
			logx.Warn("LEADER", "tryBroadcastShreds", "Failed to broadcast data shred", i, "error", err)
		}
	}

	// Then broadcast coding shreds for redundancy
	for i, shred := range codingShreds {
		if err := v.netClient.BroadcastShred(context.Background(), &shred); err != nil {
			logx.Warn("LEADER", "tryBroadcastShreds", "Failed to broadcast coding shred", i, "error", err)
		}
	}

	// Clear buffer after broadcasting
	v.entryBuffer = make([][]byte, 0)
	v.bufferSize = 0

	return nil
}

// broadcastEndOfSlotShreds broadcasts any remaining buffered entries as end-of-slot shreds
func (v *Validator) broadcastEndOfSlotShreds(slot uint64) error {
	logx.Info("LEADER", "broadcastEndOfSlotShreds", "Broadcasting", len(v.entryBuffer), "entries for slot", slot)
	if len(v.entryBuffer) == 0 {
		return nil // Nothing to broadcast
	}

	// Create signing function
	signer := NewSigner(v.PrivKey)

	// Generate final shreds with end_of_slot flag
	dataShreds, codingShreds, err := v.shredder.MakeShreds(
		slot,
		v.entryBuffer,
		signer,
		1, // version
		1, // end_of_slot flag
	)
	if err != nil {
		logx.Error("LEADER", "broadcastEndOfSlotShreds", "error", err)
		return err
	}

	logx.Info("LEADER", "broadcastEndOfSlotShreds", "Broadcasting", len(dataShreds),
		"data +", len(codingShreds), "coding shreds for slot", slot, "(end of slot)")

	// Broadcast all remaining shreds
	for i, shred := range dataShreds {
		if err := v.netClient.BroadcastShred(context.Background(), &shred); err != nil {
			logx.Warn("LEADER", "broadcastEndOfSlotShreds", "Failed to broadcast final data shred", i, "error", err)
		}
	}

	for i, shred := range codingShreds {
		if err := v.netClient.BroadcastShred(context.Background(), &shred); err != nil {
			logx.Warn("LEADER", "broadcastEndOfSlotShreds", "Failed to broadcast final coding shred", i, "error", err)
		}
	}

	// Clear buffer
	v.entryBuffer = make([][]byte, 0)
	v.bufferSize = 0

	return nil
}

func (v *Validator) onLeaderSlotStart(currentSlot uint64) {
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
				fmt.Printf("Found complete block for slot %d\n", prevSlot)
				seed, _ = v.blockStore.LastEntryInfoAtSlot(prevSlot)
				break waitLoop
			} else {
				fmt.Printf("No complete block for slot %d\n", prevSlot)
			}
		case <-deadline.C:
			fmt.Printf("Meet at deadline %d\n", prevSlot)
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

func (v *Validator) onLeaderSlotEnd() {
	v.leaderStartAtSlot = NoSlot
	v.collectedEntries = make([]poh.Entry, 0, v.BatchSize)
	// Reset shred buffer for new slot
	v.entryBuffer = make([][]byte, 0)
	v.bufferSize = 0
	v.session = v.ledger.NewSession()
	v.lastSession = v.ledger.NewSession()
}

func (v *Validator) fastForwardTicks(prevSlot uint64) blockstore.SlotBoundary {
	target := prevSlot*v.TicksPerSlot + v.TicksPerSlot - 1
	hash, _ := v.Recorder.FastForward(target)
	return blockstore.SlotBoundary{
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
	return v.IsLeader(slot)
}

// handleEntry buffers entries and broadcasts shreds when threshold is reached
func (v *Validator) handleEntry(entries []poh.Entry) {
	currentSlot := v.Recorder.CurrentSlot()

	// When slot advances, handle end of previous slot if we were leader
	if currentSlot > v.lastSlot && v.IsLeader(v.lastSlot) {
		// Broadcast any remaining entries as end-of-slot shreds
		if err := v.broadcastEndOfSlotShreds(v.lastSlot); err != nil {
			logx.Error("LEADER", "handleEntry", "Failed to broadcast end-of-slot shreds", err)
		}

		// Still create traditional block for consensus and storage
		var hash [32]byte
		if v.lastSlot == 0 {
			hash = v.blockStore.Seed()
		} else {
			entry, _ := v.blockStore.LastEntryInfoAtSlot(v.lastSlot - 1)
			hash = entry.Hash
		}

		blk := block.AssembleBlock(
			v.lastSlot,
			hash,
			v.Pubkey,
			v.collectedEntries,
		)

		blk.Sign(v.PrivKey)
		logx.Info("LEADER", "handleEntry", "Leader assembled block: slot", v.lastSlot, "entries", len(v.collectedEntries))

		// Persist the block for consensus
		v.blockStore.AddBlockPending(blk)

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
			if err := v.ledger.ApplyBlock(v.blockStore.Block(vote.Slot)); err != nil {
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
	}

	// Buffer entries only if leader of current slot
	if v.IsLeader(currentSlot) {
		v.collectedEntries = append(v.collectedEntries, entries...)
		fmt.Printf("Adding %d entries for slot %d\n", len(entries), currentSlot)

		// NEW: Also buffer entries for immediate shred broadcasting
		for _, entry := range entries {
			entryBytes, err := v.serializeEntry(entry)
			if err != nil {
				fmt.Printf("[LEADER] Failed to serialize entry: %v\n", err)
				continue
			}

			v.entryBuffer = append(v.entryBuffer, entryBytes)
			v.bufferSize += len(entryBytes)

			// Check if we should broadcast shreds now
			if err := v.tryBroadcastShreds(currentSlot); err != nil {
				fmt.Printf("[LEADER] Failed to broadcast shreds: %v\n", err)
			}
		}
	}

	// Update lastSlot
	v.lastSlot = currentSlot
}

func (v *Validator) peekPendingValidTxs(size int) []*types.Transaction {
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

	exception.SafeGoWithPanic("leaderBatchLoop", func() {
		v.leaderBatchLoop()
	})
	exception.SafeGoWithPanic("roleMonitorLoop", func() {
		v.roleMonitorLoop()
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
