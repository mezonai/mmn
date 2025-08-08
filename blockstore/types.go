package blockstore

import (
	"mmn/block"
	"sync"
)

// BlockStore manages the chain of blocks, persisting them and tracking the latest hash.
// It is safe for concurrent use.
type BlockStore struct {
	dir  string
	mu   sync.RWMutex
	data map[uint64]*block.Block

	latestFinalized uint64
	SeedHash        [32]byte
}

type SlotBoundary struct {
	Slot uint64
	Hash [32]byte
}
