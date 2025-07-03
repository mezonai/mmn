package poh

import (
	"crypto/sha256"
	"fmt"
	"sync"
)

type PohRecorder struct {
	poh            *Poh
	ticksPerSlot   uint64
	tickHeight     uint64
	currentSlot    uint64
	entries        []Entry
	mu             sync.Mutex
	leaderSchedule *LeaderSchedule
	myPubkey       string
}

// NewPohRecorder creates a new recorder that tracks PoH and turns txs into entries
func NewPohRecorder(poh *Poh, ticksPerSlot uint64, myPubkey string, schedule *LeaderSchedule) *PohRecorder {
	return &PohRecorder{
		poh:            poh,
		ticksPerSlot:   ticksPerSlot,
		tickHeight:     0,
		currentSlot:    0,
		entries:        []Entry{},
		leaderSchedule: schedule,
		myPubkey:       myPubkey,
	}
}

// RecordTxs records transactions into PoH stream
func (r *PohRecorder) RecordTxs(txs [][]byte) (*Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.poh.RemainingHashes == 1 {
		return nil, fmt.Errorf("tick required before record")
	}

	mixin := hashTransactions(txs)
	pohEntry := r.poh.Record(mixin)
	if pohEntry == nil {
		return nil, fmt.Errorf("PoH refused to record, tick required")
	}

	entry := NewTxEntry(pohEntry.NumHashes, pohEntry.Hash, txs)
	r.entries = append(r.entries, entry)
	return &entry, nil
}

// Tick advances PoH and records a tick-only entry
func (r *PohRecorder) Tick() *Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	pohEntry := r.poh.Tick()
	if pohEntry == nil {
		return nil
	}

	entry := NewTickEntry(pohEntry.NumHashes, pohEntry.Hash)
	r.entries = append(r.entries, entry)

	r.tickHeight++
	r.currentSlot = r.tickHeight / r.ticksPerSlot

	return &entry
}

// DrainEntries flushes all pending entries
func (r *PohRecorder) DrainEntries() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries := r.entries
	r.entries = nil
	return entries
}

// TickHeight returns current tick count
func (r *PohRecorder) TickHeight() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tickHeight
}

// CurrentSlot returns the slot based on tick_height
func (r *PohRecorder) CurrentSlot() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentSlot
}

// WouldBeLeader determines if this node will become leader within next n ticks
func (r *PohRecorder) WouldBeLeader(withinNextNTicks uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	targetTick := r.tickHeight + withinNextNTicks
	targetSlot := targetTick / r.ticksPerSlot
	leader, hasAssigned := r.leaderSchedule.LeaderAt(targetSlot)

	return hasAssigned && leader == r.myPubkey
}

func hashTransactions(txs [][]byte) [32]byte {
	var all []byte
	for _, tx := range txs {
		all = append(all, tx...)
	}
	return sha256.Sum256(all)
}
