package blockstore

import (
	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/events"
)

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
	AddBlockPending(b *block.Block, eventRouter *events.EventRouter) error
	MarkFinalized(slot uint64, eventRouter *events.EventRouter) error
	Seed() [32]byte
	GetTransactionBlockInfo(clientHashHex string) (slot uint64, block *block.Block, finalized bool, found bool)
	GetConfirmations(blockSlot uint64) uint64
	Close() error
}
