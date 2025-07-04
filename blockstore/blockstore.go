package blockstore

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"mmn/block"
)

// BlockStore manages the chain of blocks, persisting them and tracking the latest hash.
// It is safe for concurrent use.
type BlockStore struct {
	dir        string
	mu         sync.Mutex
	latestHash [32]byte
	latestSlot uint64
}

// NewBlockStore initializes a BlockStore, loading existing chain if present.
func NewBlockStore(dir string) (*BlockStore, error) {
	bs := &BlockStore{dir: dir}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("makedir %s: %w", dir, err)
	}
	// Try loading highest slot file
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	var maxSlot uint64
	for _, f := range files {
		var slot uint64
		if _, err := fmt.Sscanf(f.Name(), "%d.json", &slot); err == nil {
			if slot > maxSlot {
				maxSlot = slot
			}
		}
	}
	if maxSlot > 0 {
		blk, err := LoadBlock(dir, maxSlot)
		if err != nil {
			return nil, err
		}
		bs.latestHash = blk.BlockHash
		bs.latestSlot = maxSlot
	}
	return bs, nil
}

// LatestHash returns the hash of the most recently added block.
func (bs *BlockStore) LatestHash() [32]byte {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	return bs.latestHash
}

// AddBlock persists the block to disk and updates the latest hash and slot.
func (bs *BlockStore) AddBlock(b *block.Block) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	// Serialize
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal block: %w", err)
	}
	path := filepath.Join(bs.dir, fmt.Sprintf("%d.json", b.Slot))
	if err := ioutil.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write file %s: %w", path, err)
	}
	// Update latest
	bs.latestHash = b.BlockHash
	bs.latestSlot = b.Slot
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
