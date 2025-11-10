package poh

import (
	"fmt"

	"github.com/mezonai/mmn/logx"
)

// SlotHashEntry represents a slot and its corresponding hash
type SlotHashEntry struct {
	Slot uint64
	Hash [32]byte
}

type SlotHashQueue struct {
	slotMap map[uint64][32]byte // Map for O(1) slot -> hash lookup
	maxSize int                 // Maximum number of slots to keep
}

// NewSlotHashQueue creates a new slot hash queue with the specified maximum size
func NewSlotHashQueue() *SlotHashQueue {
	// Seed for slot 0
	zeroHash := [32]byte{}
	slotMap := make(map[uint64][32]byte)
	slotMap[0] = zeroHash

	return &SlotHashQueue{
		slotMap: slotMap,
		maxSize: MaxSlotHashQueueSize,
	}
}

// Put adds or updates a slot hash entry
func (q *SlotHashQueue) Put(slot uint64, hash [32]byte) {
	logx.Debug("SlotHashQueue", fmt.Sprintf("Put %d %x", slot, hash))
	// Check if slot already exists
	if _, exists := q.slotMap[slot]; exists {
		// Update existing entry
		logx.Debug("SlotHashQueue", fmt.Sprintf("Update existing entry %d %x", slot, hash))
		q.slotMap[slot] = hash
		return
	}

	// Add new entry
	q.slotMap[slot] = hash
	logx.Debug("SlotHashQueue", fmt.Sprintf("Add new entry %d %x", slot, hash))

	// Cleanup old entries if queue is full
	if len(q.slotMap) > q.maxSize {
		// Remove the oldest entry
		logx.Debug("SlotHashQueue", fmt.Sprintf("Remove oldest entry %d", slot-uint64(q.maxSize)))
		delete(q.slotMap, slot-uint64(q.maxSize))
	}
	logx.Debug("SlotHashQueue", fmt.Sprintf("Put done %d %x", slot, hash))
}

// Get retrieves the hash for a given slot
func (q *SlotHashQueue) Get(slot uint64) ([32]byte, bool) {
	hash, exists := q.slotMap[slot]
	return hash, exists
}
