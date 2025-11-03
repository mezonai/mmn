package poh

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/transaction"
)

type PohRecorder struct {
	poh            *Poh
	leaderSchedule *LeaderSchedule

	myPubkey     string
	ticksPerSlot uint64
	tickHeight   uint64
	entries      []Entry
	mu           sync.Mutex
}

// NewPohRecorder creates a new recorder that tracks PoH and turns txs into entries
func NewPohRecorder(poh *Poh, ticksPerSlot uint64, myPubkey string, schedule *LeaderSchedule, latestSlot uint64) *PohRecorder {
	return &PohRecorder{
		poh:            poh,
		ticksPerSlot:   ticksPerSlot,
		tickHeight:     latestSlot * ticksPerSlot,
		entries:        []Entry{},
		leaderSchedule: schedule,
		myPubkey:       myPubkey,
	}
}

func (r *PohRecorder) RecordTxs(txs []*transaction.Transaction) (*Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	mixin := HashTransactions(txs)
	pohEntry := r.poh.Record(mixin)
	if pohEntry == nil {
		return nil, fmt.Errorf("PoH refused to record, tick required")
	}

	entry := NewTxEntry(pohEntry.NumHashes, pohEntry.Hash, txs)
	r.entries = append(r.entries, entry)
	return &entry, nil
}

func (r *PohRecorder) Tick() {
	r.mu.Lock()
	defer r.mu.Unlock()

	pohEntry := r.poh.Tick()
	if pohEntry == nil {
		return
	}

	entry := NewTickEntry(pohEntry.NumHashes, pohEntry.Hash)

	r.tickHeight++

	logx.Debug("PohRecorder", fmt.Sprintf("Tick done %x", entry.Hash))
	r.entries = append(r.entries, entry)
}

func (r *PohRecorder) DrainEntries() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries := r.entries
	r.entries = nil
	return entries
}

// TODO: update related code to use this
func (r *PohRecorder) CurrentSlot() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	// +1 to make slot start from 1
	return r.tickHeight/r.ticksPerSlot + 1
}

func HashTransactions(txs []*transaction.Transaction) [32]byte {
	var all []byte
	for _, tx := range txs {
		all = append(all, tx.Bytes()...)
	}
	return sha256.Sum256(all)
}
