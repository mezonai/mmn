package poh

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/mezonai/mmn/types"
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
	r.tickHeight = slot * r.ticksPerSlot
	r.poh.Reset(lastHash)
	r.entries = make([]Entry, 0) //TODO: need re-calculate entries base on last hash
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

func (r *PohRecorder) RecordTxs(txs []*types.Transaction) (*Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	txHashes := make([]string, len(txs))
	for i, tx := range txs {
		txHashes[i] = tx.Hash()
	}
	mixin := HashTransactions(txs)
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

	// fmt.Println("Starting Tick")
	pohEntry := r.poh.Tick()
	if pohEntry == nil {
		return nil
	}

	entry := NewTickEntry(pohEntry.NumHashes, pohEntry.Hash)
	r.tickHash[r.tickHeight] = pohEntry.Hash

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

	// +1 to make slot start from 1
	return r.tickHeight/r.ticksPerSlot + 1
}

func HashTransactions(txs []*types.Transaction) [32]byte {
	var all []byte
	for _, tx := range txs {
		all = append(all, tx.Bytes()...)
	}
	return sha256.Sum256(all)
}
