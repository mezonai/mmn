package blockstore

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"mmn/block"
	"mmn/utils"
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

// NewBlockStore initializes a BlockStore, loading existing chain if present.
// TODO: should dynamic follow up config
func NewBlockStore(dir string, seed []byte) (*BlockStore, error) {
	bs := &BlockStore{
		dir:      dir,
		data:     make(map[uint64]*block.Block),
		SeedHash: sha256.Sum256(seed),
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

func (bs *BlockStore) Block(slot uint64) *block.Block {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return bs.data[slot]
}

func (bs *BlockStore) HasCompleteBlock(slot uint64) bool {
	_, ok := bs.data[slot]
	return ok
}

func (bs *BlockStore) LastEntryInfoAtSlot(slot uint64) (SlotBoundary, bool) {
	b, ok := bs.data[slot]
	if !ok {
		return SlotBoundary{}, false
	}

	lastEntryHash := b.LastEntryHash()
	return SlotBoundary{
		Slot: slot,
		Hash: lastEntryHash,
	}, true
}

func (bs *BlockStore) AddBlockPending(b *block.Block) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	fmt.Printf("Adding pending block %d to blockstore\n", b.Slot)

	if _, ok := bs.data[b.Slot]; ok {
		return fmt.Errorf("block %d already exists", b.Slot)
	}
	if err := bs.writeToDisk(b); err != nil {
		return err
	}
	bs.data[b.Slot] = b
	
	fmt.Printf("Pending block %d added to blockstore\n", b.Slot)
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
	
	fmt.Printf("Block %d marked as finalized\n", slot)
	fmt.Printf("Latest finalized block: %d\n", bs.latestFinalized)
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

// LatestFinalized returns the latest finalized slot number.
func (bs *BlockStore) LatestFinalized() uint64 {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return bs.latestFinalized
}



// GetTransactionHashes returns all transaction hashes for a given block slot
func (bs *BlockStore) GetTransactionHashes(slot uint64) []string {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	
	block := bs.data[slot]
	if block == nil {
		return nil
	}
	
	var txHashes []string
	for _, entry := range block.Entries {
		for _, raw := range entry.Transactions {
			tx, err := utils.ParseTx(raw)
			if err != nil {
				continue
			}
			txHashes = append(txHashes, tx.Hash())
		}
	}
	return txHashes
}

// FindTransactionByClientHash searches all stored blocks for a transaction whose
// client-computed hash (sha256 of the canonical Serialize() fields) matches the
// provided hex string. It returns the containing slot, block hash, whether the
// block is finalized, and whether it was found.
func (bs *BlockStore) FindTransactionByClientHash(clientHashHex string) (slot uint64, blockHash [32]byte, finalized bool, found bool) {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	for s, blk := range bs.data {
		for _, entry := range blk.Entries {
			for _, raw := range entry.Transactions {
				// Follow the same approach as ApplyBlock
				tx, err := utils.ParseTx(raw)
				if err != nil {
					continue
				}
				if tx.Hash() == clientHashHex {
					return s, blk.Hash, blk.Status == block.BlockFinalized, true
				}
			}
		}
	}
	return 0, [32]byte{}, false, false
}
