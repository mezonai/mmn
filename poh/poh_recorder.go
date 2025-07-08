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
		entries:        []Entry{},
		leaderSchedule: schedule,
		myPubkey:       myPubkey,
	}
}

func (r *PohRecorder) RecordTxs(txs [][]byte) (*Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	mixin := hashTransactions(txs)
	pohEntry := r.poh.Record(mixin)
	if pohEntry == nil {
		return nil, fmt.Errorf("PoH refused to record, tick required")
	}

	entry := NewTxEntry(pohEntry.NumHashes, pohEntry.Hash, txs)
	r.entries = append(r.entries, entry)
	return &entry, nil
}

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
	return r.tickHeight / r.ticksPerSlot
}

func hashTransactions(txs [][]byte) [32]byte {
	var all []byte
	for _, tx := range txs {
		all = append(all, tx...)
	}
	return sha256.Sum256(all)
}
