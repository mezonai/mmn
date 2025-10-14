package poh

import (
	"crypto/sha256"
	"encoding/binary"
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

	// Resource bounds validation
	if len(txs) > MAX_TRANSACTIONS_PER_ENTRY {
		return nil, fmt.Errorf("too many transactions: %d > %d", len(txs), MAX_TRANSACTIONS_PER_ENTRY)
	}

	if len(r.entries) >= MAX_ENTRIES_PER_SLOT {
		return nil, fmt.Errorf("slot full: %d entries >= %d", len(r.entries), MAX_ENTRIES_PER_SLOT)
	}

	if len(r.entries) >= MAX_ENTRIES_MEMORY {
		return nil, fmt.Errorf("memory limit exceeded: %d entries >= %d", len(r.entries), MAX_ENTRIES_MEMORY)
	}

	mixin := HashTransactions(txs)
	pohEntry := r.poh.Record(mixin)
	if pohEntry == nil {
		return nil, fmt.Errorf("PoH refused to record, tick required")
	}

	// Validate NumHashes bounds
	if pohEntry.NumHashes > MAX_NUM_HASHES {
		return nil, fmt.Errorf("NumHashes too large: %d > %d", pohEntry.NumHashes, MAX_NUM_HASHES)
	}

	entry := NewTxEntry(pohEntry.NumHashes, pohEntry.Hash, txs)

	// Validate entry size
	if entrySize := estimateEntrySize(entry); entrySize > MAX_ENTRY_SIZE {
		return nil, fmt.Errorf("entry too large: %d bytes > %d", entrySize, MAX_ENTRY_SIZE)
	}

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

	// Validate NumHashes bounds for tick entries
	if pohEntry.NumHashes > MAX_NUM_HASHES {
		logx.Error("PohRecorder", fmt.Sprintf("NumHashes too large in tick: %d > %d", pohEntry.NumHashes, MAX_NUM_HASHES))
		return nil
	}

	entry := NewTickEntry(pohEntry.NumHashes, pohEntry.Hash)

	r.tickHeight++
	if r.IsLastTickOfSlot() {
		logx.Debug("PohRecorder", fmt.Sprintf("Putting slot hash %d %x", r.tickHeight/r.ticksPerSlot, entry.Hash))
		r.slotHashQueue.Put(r.tickHeight/r.ticksPerSlot, entry.Hash)
	}
	logx.Debug("PohRecorder", fmt.Sprintf("Tick done %x", entry.Hash))
	return &entry
}

func (r *PohRecorder) IsLastTickOfSlot() bool {
	return r.tickHeight%r.ticksPerSlot == 0
}

func (r *PohRecorder) DrainEntries() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries := r.entries
	r.entries = nil
	return entries
}

func estimateEntrySize(entry Entry) int {
	size := 8 + 32 + 1                   // NumHashes + Hash + Tick
	size += len(entry.Transactions) * 32 // Approximate transaction size
	return size
}

func (r *PohRecorder) CurrentPassedSlot() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	// +1 to make slot start from 1
	return r.tickHeight / r.ticksPerSlot
}

func (r *PohRecorder) CurrentSlot() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	// +1 to make slot start from 1
	return r.tickHeight/r.ticksPerSlot + 1
}

func HashTransactions(txs []*transaction.Transaction) [32]byte {
	if len(txs) == 0 {
		domainHash := append([]byte(TRANSACTION_DOMAIN_PREFIX), []byte("EMPTY")...)
		return sha256.Sum256(domainHash)
	}
	var all []byte
	for _, tx := range txs {
		txBytes := tx.Bytes()
		lengthBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(lengthBytes, uint32(len(txBytes)))
		all = append(all, lengthBytes...)
		all = append(all, txBytes...)
	}
	// Apply domain separation for transaction hashes
	domainHash := append([]byte(TRANSACTION_DOMAIN_PREFIX), all...)
	return sha256.Sum256(domainHash)
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
