package validator

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"mmn/block"
	"mmn/blockstore"
	"mmn/consensus"
	"mmn/ledger"
	"mmn/mempool"
	"mmn/network"
	"mmn/poh"
)

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
	PollInterval time.Duration
	BatchSize    int

	netClient  *network.GRPCClient
	blockStore *blockstore.BlockStore
	ledger     *ledger.Ledger
	session    *ledger.Session
	collector  *consensus.Collector
	// Slot & entry buffer
	lastSlot         uint64
	collectedEntries []poh.Entry
	stopCh           chan struct{}
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
	pollInterval time.Duration,
	batchSize int,
	netClient *network.GRPCClient,
	blockStore *blockstore.BlockStore,
	ledger *ledger.Ledger,
	collector *consensus.Collector,
) *Validator {
	v := &Validator{
		Pubkey:           pubkey,
		PrivKey:          privKey,
		Recorder:         rec,
		Service:          svc,
		Schedule:         schedule,
		Mempool:          mempool,
		TicksPerSlot:     ticksPerSlot,
		PollInterval:     pollInterval,
		BatchSize:        batchSize,
		netClient:        netClient,
		blockStore:       blockStore,
		ledger:           ledger,
		session:          ledger.NewSession(),
		lastSlot:         0,
		collectedEntries: make([]poh.Entry, 0),
		collector:        collector,
	}
	svc.OnEntry = v.handleEntry
	return v
}

// isLeader checks if this validator is leader for given slot.
func (v *Validator) isLeader(slot uint64) bool {
	leader, has := v.Schedule.LeaderAt(slot)
	return has && leader == v.Pubkey
}

// handleEntry buffers entries and assembles a block at slot boundary.
func (v *Validator) handleEntry(e poh.Entry) {
	currentSlot := v.Recorder.CurrentSlot()

	// When slot advances, assemble block for lastSlot if we were leader
	if currentSlot > v.lastSlot && v.isLeader(v.lastSlot) {
		// Retrieve previous block hash from blockStore
		prevHash := v.blockStore.LatestFinalizedHash()

		blk := block.AssembleBlock(
			v.lastSlot,
			prevHash,
			v.Pubkey,
			v.collectedEntries,
		)

		if err := v.ledger.VerifyBlock(blk); err != nil {
			fmt.Printf("[LEADER] sanity verify fail: %v\n", err)
			return
		}

		blk.Sign(v.PrivKey)
		fmt.Printf("Leader assembled block: slot=%d, entries=%d\n", v.lastSlot, len(v.collectedEntries))

		// Persist then broadcast
		v.blockStore.AddBlockPending(blk)
		if err := v.netClient.BroadcastBlock(context.Background(), network.ToProtoBlock(blk)); err != nil {
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
		if committed, err := v.collector.AddVote(vote); err != nil {
			fmt.Printf("[LEADER] Add vote error: %v\n", err)
		} else if committed {
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
		v.session = v.ledger.NewSession() // reset session
	}

	// Buffer entries only if leader of current slot
	if v.isLeader(currentSlot) {
		fmt.Printf("Adding entry %x\n for slot %d\n", e.Hash, currentSlot)
		v.collectedEntries = append(v.collectedEntries, e)
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
	batchTicker := time.NewTicker(v.PollInterval)
	defer batchTicker.Stop()
	for {
		select {
		case <-v.stopCh:
			return
		case <-batchTicker.C:
			slot := v.Recorder.CurrentSlot()
			if !v.isLeader(slot) {
				continue
			}

			batch := v.Mempool.PullBatch(v.BatchSize)
			if len(batch) == 0 {
				continue
			}

			valid, errs := v.session.FilterValid(batch)
			if len(errs) > 0 {
				fmt.Println("Invalid transactions:", errs)
				continue
			}

			entry, err := v.Recorder.RecordTxs(valid)
			if err != nil {
				continue
			}
			fmt.Printf("[LEADER] Recorded %d tx (slot=%d, entry=%x...)\n", len(batch), slot, entry.Hash[:6])
		}
	}
}

func (v *Validator) roleMonitorLoop() {
	ticker := time.NewTicker(v.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			slot := v.Recorder.CurrentSlot()
			if v.isLeader(slot) {
				fmt.Println("Switched to LEADER for slot", slot, "at", time.Now().Format(time.RFC3339))
			} else {
				fmt.Println("Switched to FOLLOWER for slot", slot, "at", time.Now().Format(time.RFC3339))
			}
		}
	}
}
