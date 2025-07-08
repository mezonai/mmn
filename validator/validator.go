package validator

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"mmn/block"
	"mmn/blockstore"
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
	ticksPerSlot uint64,
	pollInterval time.Duration,
	batchSize int,
	netClient *network.GRPCClient,
	blockStore *blockstore.BlockStore,
	mempool *mempool.Mempool,
) *Validator {
	v := &Validator{
		Pubkey:           pubkey,
		PrivKey:          privKey,
		Recorder:         rec,
		Service:          svc,
		Schedule:         schedule,
		TicksPerSlot:     ticksPerSlot,
		PollInterval:     pollInterval,
		BatchSize:        batchSize,
		netClient:        netClient,
		blockStore:       blockStore,
		lastSlot:         0,
		collectedEntries: make([]poh.Entry, 0),
		Mempool:          mempool,
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
		prevHash := v.blockStore.LatestHash()
		blk := block.AssembleBlock(
			v.lastSlot,
			prevHash,
			v.Pubkey,
			v.collectedEntries,
		)
		blk.Sign(v.PrivKey)
		fmt.Printf("Leader assembled block: slot=%d, entries=%d\n", v.lastSlot, len(v.collectedEntries))
		// Persist then broadcast
		v.blockStore.AddBlock(blk)
		if err := v.netClient.BroadcastBlock(context.Background(), network.ToProtoBlock(blk)); err != nil {
			fmt.Println("Failed to broadcast block:", err)
		}
		// Reset buffer
		v.collectedEntries = make([]poh.Entry, 0, v.BatchSize)
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
			entry, err := v.Recorder.RecordTxs(batch)
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
