package blockstore

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/utils"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/logx"
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
	currentSlot     uint64 // Current processing slot
	txStore         TxStore
}

// NewGenericBlockStore creates a new generic block store with the given provider
func NewGenericBlockStore(provider DatabaseProvider, ts TxStore) (Store, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider cannot be nil")
	}

	store := &GenericBlockStore{
		provider: provider,
		txStore:  ts,
	}

	// Load existing metadata
	if err := store.loadLatestFinalized(); err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	// Load block history
	if err := store.LoadBlockHistory(); err != nil {
		logx.Error("BLOCKSTORE", "Failed to load block history:", err)
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
func (s *GenericBlockStore) AddBlockPending(b *block.BroadcastedBlock) error {
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

	// Store block
	value, err := json.Marshal(utils.BroadcastedBlockToBlock(b))
	if err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}
	if err := s.provider.Put(key, value); err != nil {
		return fmt.Errorf("failed to store block: %w", err)
	}

	// Store block tsx
	txs := make([]*transaction.Transaction, 0)
	for _, entry := range b.Entries {
		txs = append(txs, entry.Transactions...)
	}
	if err := s.txStore.StoreBatch(txs); err != nil {
		return fmt.Errorf("failed to store txs: %w", err)
	}

	// Update current slot if this block is newer
	if b.Slot > s.currentSlot {
		s.currentSlot = b.Slot
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

// Close closes the underlying database provider
func (s *GenericBlockStore) Close() error {
	return s.provider.Close()
}

// GetCurrentSlot returns the current processing slot
func (s *GenericBlockStore) GetCurrentSlot() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentSlot
}

// GetFinalizedSlot returns the latest finalized slot
func (s *GenericBlockStore) GetFinalizedSlot() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestFinalized
}

// LoadBlockHistory loads existing blocks from storage and updates current slot
func (s *GenericBlockStore) LoadBlockHistory() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Since we don't have iterator, we'll scan slots from 0 upwards
	// until we find a gap or reach a reasonable limit
	maxSlot := uint64(0)
	count := 0

	// Check slots from 0 to find the highest existing slot
	// We'll check up to a reasonable limit to avoid infinite loop
	const maxScanSlots = 100000

	consecutiveMisses := 0
	const maxConsecutiveMisses = 1000 // Stop scanning after 1000 consecutive misses

	for slot := uint64(0); slot < maxScanSlots && consecutiveMisses < maxConsecutiveMisses; slot++ {
		key := slotToBlockKey(slot)
		exists, err := s.provider.Has(key)
		if err != nil {
			logx.Error("BLOCKSTORE", "Error checking slot", slot, ":", err)
			continue
		}

		if exists {
			maxSlot = slot
			count++
			consecutiveMisses = 0
		} else {
			consecutiveMisses++
		}
	}

	s.currentSlot = maxSlot

	if count > 0 {
		logx.Info("BLOCKSTORE", fmt.Sprintf("Loaded %d blocks from history, latest slot: %d", count, maxSlot))
	}

	return nil
}

// GetBlockRange returns blocks in the specified slot range
func (s *GenericBlockStore) GetBlockRange(startSlot, endSlot uint64) ([]*block.Block, error) {
	if startSlot > endSlot {
		return nil, fmt.Errorf("invalid slot range: start %d > end %d", startSlot, endSlot)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	blocks := make([]*block.Block, 0, endSlot-startSlot+1)

	for slot := startSlot; slot <= endSlot; slot++ {
		blk := s.blockAtSlot(slot) // Use internal method to avoid double-locking
		if blk != nil {
			blocks = append(blocks, blk)
		}
	}

	return blocks, nil
}

// blockAtSlot is an internal method that doesn't acquire locks
func (s *GenericBlockStore) blockAtSlot(slot uint64) *block.Block {
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
