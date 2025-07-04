package validator

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"mmn/block"
	"mmn/blockstore"
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
	TicksPerSlot uint64

	// Configurable parameters
	PollInterval time.Duration
	BatchSize    int

	netClient  *network.GRPCClient
	blockStore *blockstore.BlockStore

	// Slot & entry buffer
	lastSlot         uint64
	collectedEntries []poh.Entry
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
	currentSlot := v.Recorder.TickHeight() / v.TicksPerSlot

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
		v.collectedEntries = v.collectedEntries[:0]
	}

	// Buffer entries only if leader of current slot
	if v.isLeader(currentSlot) {
		v.collectedEntries = append(v.collectedEntries, e)
	}

	// Update lastSlot
	v.lastSlot = currentSlot
}

// Run starts the validator loop, updating role based on current slot.
func (v *Validator) Run() {
	v.Service.Start()
	fmt.Println("Validator started. Pubkey=", v.Pubkey)

	ticker := time.NewTicker(v.PollInterval)
	defer ticker.Stop()

	for range ticker.C {
		slot := v.Recorder.TickHeight() / v.TicksPerSlot
		if v.isLeader(slot) {
			fmt.Println("Switched to LEADER for slot", slot)
		} else {
			fmt.Println("Switched to FOLLOWER for slot", slot)
		}
	}
}
