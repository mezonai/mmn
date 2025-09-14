package block

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"sync"
	"time"

	"github.com/mezonai/mmn/poh"
)

// OptimizedBlockAssembler provides faster block assembly with caching
type OptimizedBlockAssembler struct {
	hashPool sync.Pool
	bufPool  sync.Pool
}

// NewOptimizedBlockAssembler creates a new optimized block assembler
func NewOptimizedBlockAssembler() *OptimizedBlockAssembler {
	return &OptimizedBlockAssembler{
		hashPool: sync.Pool{
			New: func() interface{} {
				return sha256.New()
			},
		},
		bufPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 8)
			},
		},
	}
}

// AssembleBlockOptimized creates blocks faster with object pooling
func (oba *OptimizedBlockAssembler) AssembleBlockOptimized(
	slot uint64,
	prevHash [32]byte,
	leaderID string,
	entries []poh.Entry,
) *BroadcastedBlock {
	b := &BroadcastedBlock{
		BlockCore: BlockCore{
			Slot:      slot,
			PrevHash:  prevHash,
			LeaderID:  leaderID,
			Timestamp: uint64(time.Now().UnixNano()),
		},
		Entries: entries,
	}
	b.Hash = oba.computeHashOptimized(b)
	return b
}

// computeHashOptimized uses object pooling for faster hash computation
func (oba *OptimizedBlockAssembler) computeHashOptimized(b *BroadcastedBlock) [32]byte {
	h := oba.hashPool.Get().(hash.Hash)
	defer oba.hashPool.Put(h)
	h.Reset()

	buf := oba.bufPool.Get().([]byte)
	defer oba.bufPool.Put(buf)

	// Slot
	binary.BigEndian.PutUint64(buf, b.Slot)
	h.Write(buf)

	// PrevHash
	h.Write(b.PrevHash[:])

	// LeaderID
	h.Write([]byte(b.LeaderID))

	// Timestamp (UnixNano)
	binary.BigEndian.PutUint64(buf, b.Timestamp)
	h.Write(buf)

	// Entries (optimized loop)
	for _, e := range b.Entries {
		binary.BigEndian.PutUint64(buf, e.NumHashes)
		h.Write(buf)
		h.Write(e.Hash[:])
	}

	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// BatchAssembleBlocks assembles multiple blocks in parallel
func (oba *OptimizedBlockAssembler) BatchAssembleBlocks(
	slots []uint64,
	prevHashes [][32]byte,
	leaderIDs []string,
	entriesList [][]poh.Entry,
) []*BroadcastedBlock {
	blocks := make([]*BroadcastedBlock, len(slots))

	var wg sync.WaitGroup
	for i := range slots {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			blocks[index] = oba.AssembleBlockOptimized(
				slots[index],
				prevHashes[index],
				leaderIDs[index],
				entriesList[index],
			)
		}(i)
	}
	wg.Wait()

	return blocks
}

// PrecomputeHashes precomputes hashes for faster block assembly
func (oba *OptimizedBlockAssembler) PrecomputeHashes(entries []poh.Entry) [][32]byte {
	hashes := make([][32]byte, len(entries))

	var wg sync.WaitGroup
	for i, entry := range entries {
		wg.Add(1)
		go func(index int, e poh.Entry) {
			defer wg.Done()
			hashes[index] = e.Hash
		}(i, entry)
	}
	wg.Wait()

	return hashes
}
