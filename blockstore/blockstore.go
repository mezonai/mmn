package blockstore

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"mmn/block"
)

// BlockStore manages the chain of blocks, persisting them and tracking the latest hash.
// It is safe for concurrent use.
type BlockStore struct {
	dir  string
	mu   sync.RWMutex
	data map[uint64]*block.Block

	latestFinalized uint64
}

// NewBlockStore initializes a BlockStore, loading existing chain if present.
func NewBlockStore(dir string) (*BlockStore, error) {
	bs := &BlockStore{
		dir:  dir,
		data: make(map[uint64]*block.Block),
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, _ error) error {
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		var blk block.Block
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(b, &blk); err != nil {
			return err
		}
		bs.data[blk.Slot] = &blk
		if blk.Status == block.BlockFinalized && blk.Slot > bs.latestFinalized {
			bs.latestFinalized = blk.Slot
		}
		return nil
	})
	return bs, err
}

func (bs *BlockStore) LatestFinalizedHash() [32]byte {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	if blk := bs.data[bs.latestFinalized]; blk != nil {
		return blk.Hash
	}
	return [32]byte{}
}

func (bs *BlockStore) Block(slot uint64) *block.Block {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return bs.data[slot]
}

func (bs *BlockStore) AddBlockPending(b *block.Block) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if _, ok := bs.data[b.Slot]; ok {
		return fmt.Errorf("block %d already exists", b.Slot)
	}
	if err := bs.writeToDisk(b); err != nil {
		return err
	}
	bs.data[b.Slot] = b
	return nil
}

func (bs *BlockStore) MarkFinalized(slot uint64) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	blk, ok := bs.data[slot]
	if !ok {
		return fmt.Errorf("slot %d not found", slot)
	}
	if blk.Status == block.BlockFinalized {
		return nil // idempotent
	}
	blk.Status = block.BlockFinalized
	if err := bs.writeToDisk(blk); err != nil {
		return err
	}
	if slot > bs.latestFinalized {
		bs.latestFinalized = slot
	}
	return nil
}

// LoadBlock reads a block file by slot.
func LoadBlock(dir string, slot uint64) (*block.Block, error) {
	path := filepath.Join(dir, fmt.Sprintf("%d.json", slot))
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}
	var b block.Block
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("unmarshal block: %w", err)
	}
	return &b, nil
}

// -------- internals -------------------------------------------------------------

func (bs *BlockStore) writeToDisk(b *block.Block) error {
	file := filepath.Join(bs.dir, fmt.Sprintf("%d.json", b.Slot))
	tmp := file + ".tmp"

	bytes, err := json.Marshal(b)
	if err != nil {
		return err
	}
	if err = os.WriteFile(tmp, bytes, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file) // atomic replace
}
