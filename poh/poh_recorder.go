package poh

import (
	"crypto/sha256"
	"fmt"
	"sync"
)

type PohRecorder struct {
	poh            *Poh
	leaderSchedule *LeaderSchedule

	myPubkey     string
	ticksPerSlot uint64
	tickHeight   uint64
	entries      []Entry
	tickHash     map[uint64][32]byte
	mu           sync.Mutex
}

// NewPohRecorder creates a new recorder that tracks PoH and turns txs into entries
func NewPohRecorder(poh *Poh, ticksPerSlot uint64, myPubkey string, schedule *LeaderSchedule) *PohRecorder {
	return &PohRecorder{
		poh:            poh,
		ticksPerSlot:   ticksPerSlot,
		tickHeight:     0,
		entries:        []Entry{},
		leaderSchedule: schedule,
		myPubkey:       myPubkey,
		tickHash:       make(map[uint64][32]byte),
	}
}

func (r *PohRecorder) Reset(lastHash [32]byte, slot uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tickHeight = slot*r.ticksPerSlot + r.ticksPerSlot - 1
	r.entries = make([]Entry, 0)
	r.poh.Reset(lastHash)
}

func (p *PohRecorder) HashAtHeight(h uint64) ([32]byte, bool) {
	v, ok := p.tickHash[h]
	return v, ok && h <= p.tickHeight
}

func (r *PohRecorder) FastForward(target uint64) ([32]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.tickHeight >= target {
		if h, ok := r.HashAtHeight(target); ok {
			return h, nil
		}
		return [32]byte{}, fmt.Errorf("hash for tick %d pruned", target)
	}

	var lastHash [32]byte
	for r.tickHeight < target {
		lastHash = r.poh.RecordTick().Hash
		r.tickHash[r.tickHeight] = lastHash
		r.tickHeight++
		fmt.Printf("FastForward: %d\n", r.tickHeight)
	}
	return lastHash, nil
}

func (r *PohRecorder) ReseedAtSlot(seedHash [32]byte, slot uint64) {
	tick := slot*r.ticksPerSlot + r.ticksPerSlot - 1
	r.ReseedAtTick(seedHash, tick)
}

func (r *PohRecorder) ReseedAtTick(seedHash [32]byte, tick uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.poh.Reset(seedHash)
	r.tickHeight = tick
}

func (r *PohRecorder) RecordTxs(txs [][]byte) (*Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	mixin := HashTransactions(txs)
	pohEntry := r.poh.Record(mixin)
	fmt.Println("Recorded PoH entry:", pohEntry)
	if pohEntry == nil {
		return nil, fmt.Errorf("PoH refused to record, tick required")
	}

	entry := NewTxEntry(pohEntry.NumHashes, pohEntry.Hash, txs)
	r.entries = append(r.entries, entry)
	return &entry, nil
}

// EnsureTick ensures PoH has enough remaining hashes for recording transactions
func (r *PohRecorder) EnsureTick() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If PoH doesn't have enough remaining hashes, perform a tick
	if r.poh.GetRemainingHashes() <= 1 {
		fmt.Println("[POH] Performing tick to reset remaining hashes")
		pohEntry := r.poh.RecordTick()
		if pohEntry != nil {
			r.tickHash[r.tickHeight] = pohEntry.Hash
			r.tickHeight++
			fmt.Printf("[POH] Tick completed: tickHeight=%d\n", r.tickHeight)
		}
	}
}

func (r *PohRecorder) Tick() *Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	pohEntry := r.poh.Tick()
	if pohEntry == nil {
		return nil
	}

	entry := NewTickEntry(pohEntry.NumHashes, pohEntry.Hash)
	r.tickHash[r.tickHeight] = pohEntry.Hash

	r.tickHeight++
	// fmt.Printf("PohRecorder.Tick() incremented: tickHeight=%d, currentSlot=%d\n", r.tickHeight, r.tickHeight/r.ticksPerSlot)
	return &entry
}

func (r *PohRecorder) DrainEntries() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries := r.entries
	r.entries = nil
	return entries
}

func (r *PohRecorder) CurrentSlot() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	slot := r.tickHeight / r.ticksPerSlot
	return slot
}

// UpdateLeaderSchedule updates the recorder's leader schedule (for dynamic PoS)
func (r *PohRecorder) UpdateLeaderSchedule(schedule *LeaderSchedule) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.leaderSchedule = schedule
}

func HashTransactions(txs [][]byte) [32]byte {
	var all []byte
	for _, tx := range txs {
		all = append(all, tx...)
	}
	return sha256.Sum256(all)
}
