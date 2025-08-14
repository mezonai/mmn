package blockstore

import "github.com/mezonai/mmn/block"

// Store abstracts the block storage backend (filesystem, RocksDB, ...).
// It is the minimal interface required by validator and network layers.
type Store interface {
	Block(slot uint64) *block.Block
	HasCompleteBlock(slot uint64) bool
	LastEntryInfoAtSlot(slot uint64) (SlotBoundary, bool)
	AddBlockPending(b *block.Block) error
	MarkFinalized(slot uint64) error
	Seed() [32]byte
	LatestFinalized() uint64
	GetTransactionBlockInfo(clientHashHex string) (slot uint64, block *block.Block, finalized bool, found bool)
	GetConfirmations(blockSlot uint64) uint64
}
