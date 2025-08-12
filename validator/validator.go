package validator

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"mmn/block"
	"mmn/blockstore"
	"mmn/consensus"
	"mmn/interfaces"
	"mmn/ledger"
	"mmn/mempool"
	"mmn/p2p"
	"mmn/poh"
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
	// Slot & entry buffer
	lastSlot          uint64
	leaderStartAtSlot uint64
	collectedEntries  []poh.Entry
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
	}
	svc.OnEntry = v.handleEntry
	return v
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
}

func (v *Validator) onLeaderSlotEnd() {
	v.leaderStartAtSlot = NoSlot
	v.collectedEntries = make([]poh.Entry, 0, v.BatchSize)
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

// handleEntry buffers entries and assembles a block at slot boundary.
func (v *Validator) handleEntry(entries []poh.Entry) {
	currentSlot := v.Recorder.CurrentSlot()

	// When slot advances, assemble block for lastSlot if we were leader
	if currentSlot > v.lastSlot && v.IsLeader(v.lastSlot) {
		// Retrieve previous block hash from blockStore
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

		if err := v.ledger.VerifyBlock(blk); err != nil {
			fmt.Printf("[LEADER] sanity verify fail: %v\n", err)
			v.session = v.lastSession.CopyWithOverlayClone()
			return
		}

		blk.Sign(v.PrivKey)
		fmt.Printf("Leader assembled block: slot=%d, entries=%d\n", v.lastSlot, len(v.collectedEntries))

		// Persist then broadcast
		v.blockStore.AddBlockPending(blk)
		if err := v.netClient.BroadcastBlock(context.Background(), blk); err != nil {
			fmt.Println("Failed to broadcast block:", err)
		}

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
	}

	// Update lastSlot
	v.lastSlot = currentSlot
}

func (v *Validator) Run() {
	v.stopCh = make(chan struct{})

	go v.leaderBatchLoop()
	go v.roleMonitorLoop()
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
			if len(batch) == 0 {
				fmt.Println("[LEADER] No batch")
				continue
			}

			previousSession := v.session.CopyWithOverlayClone()
			fmt.Println("[LEADER] Filtering batch")
			valids, errs := v.session.FilterValid(batch)
			if len(errs) > 0 {
				fmt.Println("[LEADER] Invalid transactions:", errs)
			}

			fmt.Println("[LEADER] Recording batch")
			entry, err := v.Recorder.RecordTxs(valids)
			if err != nil {
				fmt.Println("[LEADER] Record error:", err)
				v.session = previousSession
				continue
			}
			fmt.Printf("[LEADER] Recorded %d tx (slot=%d, entry=%x...)\n", len(valids), slot, entry.Hash[:6])
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
				fmt.Println("Switched to LEADER for slot", slot, "at", time.Now().Format(time.RFC3339))
				if v.leaderStartAtSlot == NoSlot {
					v.onLeaderSlotStart(slot)
				}
			} else {
				fmt.Println("Switched to FOLLOWER for slot", slot, "at", time.Now().Format(time.RFC3339))
				if v.leaderStartAtSlot != NoSlot {
					v.onLeaderSlotEnd()
				}
			}
		}
	}
}
