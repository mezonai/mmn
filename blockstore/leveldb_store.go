// LevelDB store implementation - always available

package blockstore

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/mezonai/mmn/block"
)

const (
	// Key prefixes
	prefixMeta   = "meta:"
	prefixBlocks = "blocks:"

	// Metadata keys
	keyLatestFinalized = "latest_finalized"

	// Key sizes
	slotKeySize = 8
)

// LevelDBStore is a LevelDB-backed implementation for storing blocks and metadata.
// It mirrors the behavior of BlockStore but persists into LevelDB.
// Key prefixes:
// - meta: metadata (e.g., latest_finalized)
// - blocks: key = blocks:slot (uint64 BE), value = json(block.Block)
type LevelDBStore struct {
	dir             string
	mu              sync.RWMutex
	db              *leveldb.DB
	latestFinalized uint64
	seedHash        [32]byte
}

// NewLevelDBStore opens (or creates) a LevelDB database at dir.
func NewLevelDBStore(dir string, seed []byte) (Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("directory path cannot be empty")
	}

	db, err := leveldb.OpenFile(filepath.Clean(dir), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open LevelDB at %s: %w", dir, err)
	}

	store := &LevelDBStore{
		dir:      dir,
		db:       db,
		seedHash: sha256.Sum256(seed),
	}

	if err := store.loadLatestFinalized(); err != nil {
		store.Close()
		return nil, fmt.Errorf("failed to load latest finalized slot: %w", err)
	}

	return store, nil
}

// loadLatestFinalized loads the latest finalized slot from metadata
func (s *LevelDBStore) loadLatestFinalized() error {
	key := prefixMeta + keyLatestFinalized
	val, err := s.db.Get([]byte(key), nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			// Key doesn't exist, use default value
			return nil
		}
		return fmt.Errorf("failed to get latest finalized key: %w", err)
	}

	if len(val) == slotKeySize {
		s.latestFinalized = binary.BigEndian.Uint64(val)
	}

	return nil
}



// slotToKey converts slot number to LevelDB key with blocks prefix
func slotToKey(slot uint64) []byte {
	slotBytes := make([]byte, slotKeySize)
	binary.BigEndian.PutUint64(slotBytes, slot)
	return append([]byte(prefixBlocks), slotBytes...)
}

// getDB returns the database with read lock
func (s *LevelDBStore) getDB() *leveldb.DB {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db
}

// Block retrieves a block by slot number
func (s *LevelDBStore) Block(slot uint64) *block.Block {
	db := s.getDB()
	if db == nil {
		return nil
	}

	key := slotToKey(slot)
	val, err := db.Get(key, nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			return nil
		}
		return nil
	}

	var b block.Block
	if err := json.Unmarshal(val, &b); err != nil {
		return nil
	}

	return &b
}

// HasCompleteBlock checks if a block exists for the given slot
func (s *LevelDBStore) HasCompleteBlock(slot uint64) bool {
	db := s.getDB()
	if db == nil {
		return false
	}

	key := slotToKey(slot)
	_, err := db.Get(key, nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			return false
		}
		return false
	}

	return true
}

// LastEntryInfoAtSlot returns the slot boundary information
func (s *LevelDBStore) LastEntryInfoAtSlot(slot uint64) (SlotBoundary, bool) {
	b := s.Block(slot)
	if b == nil {
		return SlotBoundary{}, false
	}

	return SlotBoundary{
		Slot: slot,
		Hash: b.LastEntryHash(),
	}, true
}

// AddBlockPending adds a new block to the store
func (s *LevelDBStore) AddBlockPending(b *block.Block) error {
	if b == nil {
		return fmt.Errorf("block cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return fmt.Errorf("LevelDB is closed")
	}

	key := slotToKey(b.Slot)

	// Check if block already exists
	if err := s.checkBlockExists(key, b.Slot); err != nil {
		return err
	}

	// Marshal and write block
	bytes, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}

	return s.writeBlock(key, bytes)
}

// checkBlockExists verifies that a block doesn't already exist
func (s *LevelDBStore) checkBlockExists(key []byte, slot uint64) error {
	_, err := s.db.Get(key, nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			// Block doesn't exist, which is what we want
			return nil
		}
		return fmt.Errorf("failed to check block existence: %w", err)
	}

	// Block exists
	return fmt.Errorf("block %d already exists", slot)
}

// writeBlock writes a block to LevelDB
func (s *LevelDBStore) writeBlock(key, value []byte) error {
	return s.db.Put(key, value, nil)
}

// MarkFinalized marks a block as finalized
func (s *LevelDBStore) MarkFinalized(slot uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return fmt.Errorf("LevelDB is closed")
	}

	key := slotToKey(slot)

	// Get and update block
	blk, err := s.getAndUpdateBlock(key, slot)
	if err != nil {
		return err
	}

	// Write updated block and metadata
	return s.writeFinalizedBlock(key, blk, slot)
}

// getAndUpdateBlock retrieves and updates block status
func (s *LevelDBStore) getAndUpdateBlock(key []byte, slot uint64) (*block.Block, error) {
	val, err := s.db.Get(key, nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			return nil, fmt.Errorf("block at slot %d not found", slot)
		}
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	var blk block.Block
	if err := json.Unmarshal(val, &blk); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}

	if blk.Status != block.BlockFinalized {
		blk.Status = block.BlockFinalized
	}

	return &blk, nil
}

// writeFinalizedBlock writes the finalized block and updates metadata
func (s *LevelDBStore) writeFinalizedBlock(key []byte, blk *block.Block, slot uint64) error {
	bytes, err := json.Marshal(blk)
	if err != nil {
		return fmt.Errorf("failed to marshal finalized block: %w", err)
	}

	// Use a batch for atomic writes
	batch := new(leveldb.Batch)
	batch.Put(key, bytes)

	// Update latest finalized if needed
	if slot > s.latestFinalized {
		s.latestFinalized = slot
		lf := make([]byte, slotKeySize)
		binary.BigEndian.PutUint64(lf, s.latestFinalized)
		metaKey := prefixMeta + keyLatestFinalized
		batch.Put([]byte(metaKey), lf)
	}

	return s.db.Write(batch, nil)
}

// Seed returns the seed hash used for the initial previous-hash
func (s *LevelDBStore) Seed() [32]byte {
	return s.seedHash
}

// Close closes the LevelDB store and returns any error
func (s *LevelDBStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if s.db != nil {
		err := s.db.Close()
		s.db = nil
		return err
	}
	return nil
}
