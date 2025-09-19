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

	myPubkey      string
	ticksPerSlot  uint64
	tickHeight    uint64
	entries       []Entry
	slotHashQueue *SlotHashQueue
	mu            sync.Mutex
}

// NewPohRecorder creates a new recorder that tracks PoH and turns txs into entries
func NewPohRecorder(poh *Poh, ticksPerSlot uint64, myPubkey string, schedule *LeaderSchedule, latestSlot uint64) *PohRecorder {
	return &PohRecorder{
		poh:            poh,
		ticksPerSlot:   ticksPerSlot,
		tickHeight:     latestSlot * ticksPerSlot,
		entries:        []Entry{},
		slotHashQueue:  NewSlotHashQueue(),
		leaderSchedule: schedule,
		myPubkey:       myPubkey,
	}
}

func (r *PohRecorder) Reset(lastHash [32]byte, slot uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tickHeight = slot * r.ticksPerSlot
	r.poh.Reset(lastHash)
	r.entries = make([]Entry, 0)
	r.slotHashQueue.Put(slot, lastHash)
}

// Assume fromSlot is the last seen slot, toSlot is the target slot
// Simulate the poh clock from fromSlot to toSlot
func (r *PohRecorder) FastForward(seenHash [32]byte, fromSlot uint64, toSlot uint64) [32]byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	fromTick := fromSlot * r.ticksPerSlot
	toTick := toSlot * r.ticksPerSlot
	r.poh.TickFastForward(seenHash, fromTick, toTick)

	return r.poh.Hash
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

func (r *PohRecorder) Tick() *Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	pohEntry := r.poh.Tick()
	if pohEntry == nil {
		return nil
	}

	entry := NewTickEntry(pohEntry.NumHashes, pohEntry.Hash)

	r.tickHeight++
	if r.isLastTickOfSlot() {
		logx.Debug("PohRecorder", fmt.Sprintf("Putting slot hash %d %x", r.tickHeight/r.ticksPerSlot, entry.Hash))
		r.slotHashQueue.Put(r.tickHeight/r.ticksPerSlot, entry.Hash)
	}
	logx.Debug("PohRecorder", fmt.Sprintf("Tick done %x", entry.Hash))
	return &entry
}

func (r *PohRecorder) isLastTickOfSlot() bool {
	return r.tickHeight%r.ticksPerSlot == 0
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

func HashTransactions(txs []*transaction.Transaction) [32]byte {
	hasher := sha256.New()
	for _, tx := range txs {
		hasher.Write(tx.Bytes())
	}
	var result [32]byte
	hasher.Sum(result[:0])
	return result
}

func (r *PohRecorder) GetSlotHash(slot uint64) [32]byte {
	hash, ok := r.slotHashQueue.Get(slot)
	if !ok {
		logx.Warn("PohRecorder", fmt.Sprintf("Slot hash not found for slot %d", slot))
		return [32]byte{}
	}
	return hash
}

// GetSlotHashFromQueue returns slot hash from in-memory queue if available
func (r *PohRecorder) GetSlotHashFromQueue(slot uint64) ([32]byte, bool) {
	return r.slotHashQueue.Get(slot)
}
