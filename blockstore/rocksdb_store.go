//go:build rocksdb
// +build rocksdb

package blockstore

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/linxGnu/grocksdb"

	"github.com/mezonai/mmn/block"
)

const (
	// Column family names
	cfDefault = "default"
	cfBlocks  = "blocks"

	// Metadata keys
	keyLatestFinalized = "latest_finalized"

	// Key sizes
	slotKeySize = 8
)

// RocksDBStore is a RocksDB-backed implementation for storing blocks and metadata.
// It mirrors the behavior of BlockStore but persists into RocksDB.
// Column families:
// - default: metadata (e.g., latest_finalized)
// - blocks:  key = slot (uint64 BE), value = json(block.Block)
type RocksDBStore struct {
	dir             string
	mu              sync.RWMutex
	db              *grocksdb.DB
	metaCF          *grocksdb.ColumnFamilyHandle
	blocksCF        *grocksdb.ColumnFamilyHandle
	latestFinalized uint64
	seedHash        [32]byte
}

// NewRocksDBStore opens (or creates) a RocksDB database at dir.
func NewRocksDBStore(dir string, seed []byte) (Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("directory path cannot be empty")
	}

	opts := grocksdb.NewDefaultOptions()
	opts.SetCreateIfMissing(true)
	opts.SetCreateIfMissingColumnFamilies(true)

	cfNames := []string{cfDefault, cfBlocks}
	cfOpts := []*grocksdb.Options{opts, opts}

	db, handles, err := grocksdb.OpenDbColumnFamilies(opts, filepath.Clean(dir), cfNames, cfOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to open RocksDB at %s: %w", dir, err)
	}

	store := &RocksDBStore{
		dir:      dir,
		db:       db,
		metaCF:   handles[0],
		blocksCF: handles[1],
		seedHash: sha256.Sum256(seed),
	}

	if err := store.loadLatestFinalized(); err != nil {
		store.Close()
		return nil, fmt.Errorf("failed to load latest finalized slot: %w", err)
	}

	return store, nil
}

// loadLatestFinalized loads the latest finalized slot from metadata
func (s *RocksDBStore) loadLatestFinalized() error {
	ro := grocksdb.NewDefaultReadOptions()
	defer ro.Destroy()

	val, err := s.db.GetCF(ro, s.metaCF, []byte(keyLatestFinalized))
	if err != nil {
		return fmt.Errorf("failed to get latest finalized key: %w", err)
	}
	defer val.Free()

	if val.Exists() && len(val.Data()) == slotKeySize {
		s.latestFinalized = binary.BigEndian.Uint64(val.Data())
	}

	return nil
}

// Close releases all RocksDB resources
func (s *RocksDBStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.blocksCF != nil {
		s.blocksCF.Destroy()
		s.blocksCF = nil
	}
	if s.metaCF != nil {
		s.metaCF.Destroy()
		s.metaCF = nil
	}
	if s.db != nil {
		s.db.Close()
		s.db = nil
	}
}

// slotToKey converts slot number to RocksDB key
func slotToKey(slot uint64) []byte {
	key := make([]byte, slotKeySize)
	binary.BigEndian.PutUint64(key, slot)
	return key
}

// getDBAndCF returns the database and column family handles with read lock
func (s *RocksDBStore) getDBAndCF() (*grocksdb.DB, *grocksdb.ColumnFamilyHandle) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db, s.blocksCF
}

// Block retrieves a block by slot number
func (s *RocksDBStore) Block(slot uint64) *block.Block {
	db, blocksCF := s.getDBAndCF()
	if db == nil {
		return nil
	}

	key := slotToKey(slot)
	ro := grocksdb.NewDefaultReadOptions()
	defer ro.Destroy()

	val, err := db.GetCF(ro, blocksCF, key)
	if err != nil {
		return nil
	}
	defer val.Free()

	if !val.Exists() {
		return nil
	}

	var b block.Block
	if err := json.Unmarshal(val.Data(), &b); err != nil {
		return nil
	}

	return &b
}

// HasCompleteBlock checks if a block exists for the given slot
func (s *RocksDBStore) HasCompleteBlock(slot uint64) bool {
	db, blocksCF := s.getDBAndCF()
	if db == nil {
		return false
	}

	key := slotToKey(slot)
	ro := grocksdb.NewDefaultReadOptions()
	defer ro.Destroy()

	val, err := db.GetCF(ro, blocksCF, key)
	if err != nil {
		return false
	}
	defer val.Free()

	return val.Exists()
}

// GetLatestSlot returns the highest slot number that has a complete block
func (s *RocksDBStore) GetLatestSlot() uint64 {
	db, blocksCF := s.getDBAndCF()
	if db == nil {
		return 0
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	ro := grocksdb.NewDefaultReadOptions()
	defer ro.Destroy()

	// Use RocksDB iterator to find the highest slot
	it := db.NewIteratorCF(ro, blocksCF)
	defer it.Close()

	maxSlot := uint64(0)
	it.SeekToLast()
	if it.Valid() {
		key := it.Key()
		if len(key.Data()) == slotKeySize {
			maxSlot = binary.BigEndian.Uint64(key.Data())
		}
		key.Free()
	}

	return maxSlot
}

// LastEntryInfoAtSlot returns the slot boundary information
func (s *RocksDBStore) LastEntryInfoAtSlot(slot uint64) (SlotBoundary, bool) {
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
func (s *RocksDBStore) AddBlockPending(b *block.Block) error {
	if b == nil {
		return fmt.Errorf("block cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return fmt.Errorf("RocksDB is closed")
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
func (s *RocksDBStore) checkBlockExists(key []byte, slot uint64) error {
	ro := grocksdb.NewDefaultReadOptions()
	defer ro.Destroy()

	val, err := s.db.GetCF(ro, s.blocksCF, key)
	if err != nil {
		return fmt.Errorf("failed to check block existence: %w", err)
	}
	defer val.Free()

	if val.Exists() {
		return fmt.Errorf("block %d already exists", slot)
	}

	return nil
}

// writeBlock writes a block to RocksDB
func (s *RocksDBStore) writeBlock(key, value []byte) error {
	wb := grocksdb.NewWriteBatch()
	defer wb.Destroy()

	wb.PutCF(s.blocksCF, key, value)

	wo := grocksdb.NewDefaultWriteOptions()
	defer wo.Destroy()

	return s.db.Write(wo, wb)
}

// MarkFinalized marks a block as finalized
func (s *RocksDBStore) MarkFinalized(slot uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return fmt.Errorf("RocksDB is closed")
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
func (s *RocksDBStore) getAndUpdateBlock(key []byte, slot uint64) (*block.Block, error) {
	ro := grocksdb.NewDefaultReadOptions()
	defer ro.Destroy()

	val, err := s.db.GetCF(ro, s.blocksCF, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}
	defer val.Free()

	if !val.Exists() {
		return nil, fmt.Errorf("block at slot %d not found", slot)
	}

	var blk block.Block
	if err := json.Unmarshal(val.Data(), &blk); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}

	if blk.Status != block.BlockFinalized {
		blk.Status = block.BlockFinalized
	}

	return &blk, nil
}

// writeFinalizedBlock writes the finalized block and updates metadata
func (s *RocksDBStore) writeFinalizedBlock(key []byte, blk *block.Block, slot uint64) error {
	bytes, err := json.Marshal(blk)
	if err != nil {
		return fmt.Errorf("failed to marshal finalized block: %w", err)
	}

	wb := grocksdb.NewWriteBatch()
	defer wb.Destroy()

	wb.PutCF(s.blocksCF, key, bytes)

	// Update latest finalized if needed
	if slot > s.latestFinalized {
		s.latestFinalized = slot
		lf := slotToKey(s.latestFinalized)
		wb.PutCF(s.metaCF, []byte(keyLatestFinalized), lf)
	}

	wo := grocksdb.NewDefaultWriteOptions()
	defer wo.Destroy()

	return s.db.Write(wo, wb)
}

// Seed returns the seed hash used for the initial previous-hash
func (s *RocksDBStore) Seed() [32]byte {
	return s.seedHash
}
