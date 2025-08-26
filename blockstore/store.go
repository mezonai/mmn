package blockstore

import "github.com/mezonai/mmn/block"

// SlotBoundary represents slot boundary information
type SlotBoundary struct {
	Slot uint64
	Hash [32]byte
}

// Store abstracts the block storage backend (filesystem, RocksDB, ...).
// It is the minimal interface required by validator and network layers.
type Store interface {
	Block(slot uint64) *block.Block
	HasCompleteBlock(slot uint64) bool
	LastEntryInfoAtSlot(slot uint64) (SlotBoundary, bool)
	GetLatestSlot() uint64
	AddBlockPending(b *block.BroadcastedBlock) error
	MarkFinalized(slot uint64) error
	Close() error
}
