package blockstore

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/utils"
)

const (
	// Key prefixes for generic store
	genericPrefixMeta   = "meta:"
	genericPrefixBlocks = "blocks:"

	// Metadata keys
	genericKeyLatestFinalized = "latest_finalized"
)

// GenericBlockStore is a database-agnostic implementation that uses DatabaseProvider
// This allows it to work with any database backend (LevelDB, RocksDB, etc.)
type GenericBlockStore struct {
	provider        DatabaseProvider
	mu              sync.RWMutex
	latestFinalized uint64
	seedHash        [32]byte
}

// NewGenericBlockStore creates a new generic block store with the given provider
func NewGenericBlockStore(provider DatabaseProvider, seed []byte) (Store, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider cannot be nil")
	}

	store := &GenericBlockStore{
		provider: provider,
		seedHash: sha256.Sum256(seed),
	}

	// Load existing metadata
	if err := store.loadLatestFinalized(); err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	return store, nil
}

// loadLatestFinalized loads the latest finalized slot from the database
func (s *GenericBlockStore) loadLatestFinalized() error {
	key := []byte(genericPrefixMeta + genericKeyLatestFinalized)
	value, err := s.provider.Get(key)
	if err != nil {
		return fmt.Errorf("failed to get latest finalized: %w", err)
	}

	if value == nil {
		// No existing data, start from 0
		s.latestFinalized = 0
		return nil
	}

	if len(value) != 8 {
		return fmt.Errorf("invalid latest finalized value length: %d", len(value))
	}

	s.latestFinalized = binary.BigEndian.Uint64(value)
	return nil
}

// slotToBlockKey converts a slot number to a block storage key
func slotToBlockKey(slot uint64) []byte {
	key := make([]byte, len(genericPrefixBlocks)+8)
	copy(key, genericPrefixBlocks)
	binary.BigEndian.PutUint64(key[len(genericPrefixBlocks):], slot)
	return key
}

// Block retrieves a block by slot number
func (s *GenericBlockStore) Block(slot uint64) *block.Block {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := slotToBlockKey(slot)
	value, err := s.provider.Get(key)
	if err != nil {
		logx.Error("BLOCKSTORE", "Failed to get block", slot, "error:", err)
		return nil
	}

	if value == nil {
		return nil
	}

	var blk block.Block
	if err := json.Unmarshal(value, &blk); err != nil {
		logx.Error("BLOCKSTORE", "Failed to unmarshal block", slot, "error:", err)
		return nil
	}

	return &blk
}

// HasCompleteBlock checks if a complete block exists at the given slot
func (s *GenericBlockStore) HasCompleteBlock(slot uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := slotToBlockKey(slot)
	exists, err := s.provider.Has(key)
	if err != nil {
		logx.Error("BLOCKSTORE", "Failed to check block existence", slot, "error:", err)
		return false
	}

	return exists
}

// GetLatestSlot returns the latest finalized slot
func (s *GenericBlockStore) GetLatestSlot() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestFinalized
}

// LastEntryInfoAtSlot returns the slot boundary information for the given slot
func (s *GenericBlockStore) LastEntryInfoAtSlot(slot uint64) (SlotBoundary, bool) {
	blk := s.Block(slot)
	if blk == nil {
		return SlotBoundary{}, false
	}

	lastEntryHash := blk.LastEntryHash()
	return SlotBoundary{
		Slot: slot,
		Hash: lastEntryHash,
	}, true
}

// AddBlockPending adds a pending block to the store
func (s *GenericBlockStore) AddBlockPending(b *block.Block) error {
	if b == nil {
		return fmt.Errorf("block cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := slotToBlockKey(b.Slot)

	// Check if block already exists
	exists, err := s.provider.Has(key)
	if err != nil {
		return fmt.Errorf("failed to check block existence: %w", err)
	}

	if exists {
		return fmt.Errorf("block at slot %d already exists", b.Slot)
	}

	// Serialize block
	value, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}

	// Store block
	if err := s.provider.Put(key, value); err != nil {
		return fmt.Errorf("failed to store block: %w", err)
	}

	logx.Info("BLOCKSTORE", "Added pending block at slot", b.Slot)

	return nil
}

// MarkFinalized marks a block as finalized and updates metadata
func (s *GenericBlockStore) MarkFinalized(slot uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if block exists
	key := slotToBlockKey(slot)
	exists, err := s.provider.Has(key)
	if err != nil {
		return fmt.Errorf("failed to check block existence: %w", err)
	}

	if !exists {
		return fmt.Errorf("block at slot %d does not exist", slot)
	}

	// Update latest finalized
	s.latestFinalized = slot

	// Store updated metadata
	metaKey := []byte(genericPrefixMeta + genericKeyLatestFinalized)
	metaValue := make([]byte, 8)
	binary.BigEndian.PutUint64(metaValue, slot)

	if err := s.provider.Put(metaKey, metaValue); err != nil {
		return fmt.Errorf("failed to update latest finalized: %w", err)
	}

	logx.Info("BLOCKSTORE", "Marked block as finalized at slot", slot)

	return nil
}

// Seed returns the seed hash
func (s *GenericBlockStore) Seed() [32]byte {
	return s.seedHash
}

// Close closes the underlying database provider
func (s *GenericBlockStore) Close() error {
	return s.provider.Close()
}

// GetConfirmations calculates the number of confirmations for a transaction in a given block slot.
// Confirmations = latestFinalized - blockSlot + 1 if the block is finalized,
// otherwise returns 1 for confirmed but not finalized blocks.
func (bs *GenericBlockStore) GetConfirmations(blockSlot uint64) uint64 {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	latest := bs.latestFinalized
	if latest >= blockSlot {
		return latest - blockSlot + 1
	}
	return 1 // Confirmed but not yet finalized
}

// GetTransactionBlockInfo searches all stored blocks for a transaction. It returns the containing slot, the whole block, whether the
// block is finalized, and whether it was found.
func (bs *GenericBlockStore) GetTransactionBlockInfo(clientHashHex string) (slot uint64, blk *block.Block, finalized bool, found bool) {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	for s, blockData := range bs.data {
		for _, entry := range blockData.Entries {
			for _, raw := range entry.Transactions {
				tx, err := utils.ParseTx(raw)
				if err != nil {
					continue
				}
				if tx.Hash() == clientHashHex {
					return s, blockData, blockData.Status == block.BlockFinalized, true
				}
			}
		}
	}
	return 0, nil, false, false
}
