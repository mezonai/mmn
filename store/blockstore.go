package store

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mezonai/mmn/db"

	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/utils"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/logx"
)

const (
	BlockMetaKeyLatestFinalized = "latest_finalized"
)

// SlotBoundary represents slot boundary information
type SlotBoundary struct {
	Slot uint64
	Hash [32]byte
}

// BlockStore abstracts the block storage backend (filesystem, RocksDB, ...).
// It is the minimal interface required by validator and network layers.
type BlockStore interface {
	Block(slot uint64) *block.Block
	HasCompleteBlock(slot uint64) bool
	LastEntryInfoAtSlot(slot uint64) (SlotBoundary, bool)
	GetCurrentSlot() uint64   // Get current processing slot
	GetLatestSlot() uint64
	AddBlockPending(b *block.BroadcastedBlock) error
	MarkFinalized(slot uint64) error
	GetTransactionBlockInfo(clientHashHex string) (slot uint64, block *block.Block, finalized bool, found bool)
	GetConfirmations(blockSlot uint64) uint64
	MustClose()
}

// GenericBlockStore is a database-agnostic implementation that uses DatabaseProvider
// This allows it to work with any database backend (LevelDB, RocksDB, etc.)
type GenericBlockStore struct {
	provider        db.DatabaseProvider
	mu              sync.RWMutex
	latestFinalized uint64
	txStore         TxStore
	eventRouter     *events.EventRouter
	currentSlot     uint64
}

// NewGenericBlockStore creates a new generic block store with the given provider
func NewGenericBlockStore(provider db.DatabaseProvider, ts TxStore, eventRouter *events.EventRouter) (BlockStore, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider cannot be nil")
	}

	store := &GenericBlockStore{
		provider:    provider,
		txStore:     ts,
		eventRouter: eventRouter,
	}

	// Load existing metadata
	if err := store.loadLatestFinalized(); err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	return store, nil
}

// loadLatestFinalized loads the latest finalized slot from the database
func (s *GenericBlockStore) loadLatestFinalized() error {
	key := []byte(PrefixBlockMeta + BlockMetaKeyLatestFinalized)
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
	key := make([]byte, len(PrefixBlock)+8)
	copy(key, PrefixBlock)
	binary.BigEndian.PutUint64(key[len(PrefixBlock):], slot)
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

// GetCurrentSlot returns the current processing slot
func (s *GenericBlockStore) GetCurrentSlot() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentSlot
}

// GetLatestSlot returns the latest finalized slot
func (s *GenericBlockStore) GetLatestSlot() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestFinalized
}

func (s *GenericBlockStore) Close() error {
	return s.provider.Close()
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
	// TODO: storing block & its tsx should be atomic operation. Consider use batch or db transaction (if supported)
	txs := make([]*transaction.Transaction, 0)
	for _, entry := range b.Entries {
		txs = append(txs, entry.Transactions...)
	}
	if err := s.txStore.StoreBatch(txs); err != nil {
		return fmt.Errorf("failed to store txs: %w", err)
	}

	// Publish transaction inclusion events if event router is provided
	if s.eventRouter != nil {
		blockHashHex := b.HashString()

		// Publish TransactionIncludedInBlock events for each transaction in the block
		for _, entry := range b.Entries {
			for _, tx := range entry.Transactions {
				event := events.NewTransactionIncludedInBlock(tx.Hash(), b.Slot, blockHashHex)
				s.eventRouter.PublishTransactionEvent(event)
			}
		}
	}

	logx.Info("BLOCKSTORE", "Added pending block at slot", b.Slot)

	return nil
}

// MarkFinalized marks a block as finalized and updates metadata
func (s *GenericBlockStore) MarkFinalized(slot uint64) error {
	if !s.HasCompleteBlock(slot) {
		return fmt.Errorf("block at slot %d does not exist", slot)
	}

	// Get block data only if event router is provided
	var blk *block.Block
	if s.eventRouter != nil {
		blk = s.Block(slot)
		if blk == nil {
			return fmt.Errorf("failed to get block data for slot %d", slot)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Update latest finalized
	s.latestFinalized = slot

	// Store updated metadata
	metaKey := []byte(PrefixBlockMeta + BlockMetaKeyLatestFinalized)
	metaValue := make([]byte, 8)
	binary.BigEndian.PutUint64(metaValue, slot)

	if err := s.provider.Put(metaKey, metaValue); err != nil {
		return fmt.Errorf("failed to update latest finalized: %w", err)
	}

	// Publish transaction finalization events if event router is provided
	if s.eventRouter != nil && blk != nil {
		blockHashHex := blk.HashString()

		for _, entry := range blk.Entries {
			txs, err := s.txStore.GetBatch(entry.TxHashes)
			if err != nil {
				logx.Warn("BLOCKSTORE", "Failed to get transactions for finalization event", "slot", slot, "error", err)
				continue
			}
			for _, tx := range txs {
				event := events.NewTransactionFinalized(tx.Hash(), slot, blockHashHex)
				s.eventRouter.PublishTransactionEvent(event)
			}
		}
	}

	logx.Info("BLOCKSTORE", "Marked block as finalized at slot", slot)

	return nil
}

// MustClose Close closes the underlying database provider
func (s *GenericBlockStore) MustClose() {
	err := s.provider.Close()
	if err != nil {
		logx.Error("BLOCK_STORE", "Failed to close provider")
	}
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

	// Iterate through all slots from 0 to latest finalized to search for the transaction
	latestSlot := bs.latestFinalized
	for s := uint64(0); s <= latestSlot; s++ {
		key := slotToBlockKey(s)
		value, err := bs.provider.Get(key)
		if err != nil {
			logx.Error("BLOCKSTORE", "Failed to get block for transaction search", s, "error:", err)
			continue
		}

		if value == nil {
			continue
		}

		var blockData block.Block
		if err := json.Unmarshal(value, &blockData); err != nil {
			logx.Error("BLOCKSTORE", "Failed to unmarshal block for transaction search", s, "error:", err)
			continue
		}

		// Search through all entries in the block
		for _, entry := range blockData.Entries {
			for _, tx := range entry.TxHashes {
				tx, err := bs.txStore.GetByHash(tx)
				if err != nil {
					logx.Warn("BLOCKSTORE", "Failed to get transaction for transaction search", "slot", s, "error", err)
					continue
				}
				if tx.Hash() == clientHashHex {
					return s, &blockData, blockData.Status == block.BlockFinalized, true
				}
			}
		}
	}
	return 0, nil, false, false
}
