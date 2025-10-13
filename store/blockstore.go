package store

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"sync/atomic"

	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/monitoring"
	"github.com/mezonai/mmn/types"

	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/utils"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
)

const (
	UNINT64_BYTES = 8
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
	GetBatch(slots []uint64) (map[uint64]*block.Block, error)
	HasCompleteBlock(slot uint64) bool
	LastEntryInfoAtSlot(slot uint64) (SlotBoundary, bool)
	GetLatestFinalizedSlot() uint64
	GetLatestStoreSlot() uint64
	AddBlockPending(b *block.BroadcastedBlock) error
	MarkFinalized(slot uint64) error
	GetTransactionBlockInfo(clientHashHex string) (slot uint64, block *block.Block, finalized bool, found bool)
	GetConfirmations(blockSlot uint64) uint64
	MustClose()
	IsApplied(slot uint64) bool
}

// GenericBlockStore is a database-agnostic implementation that uses DatabaseProvider
// This allows it to work with any database backend (LevelDB, RocksDB, etc.)
type GenericBlockStore struct {
	provider db.DatabaseProvider

	latestFinalized atomic.Uint64
	latestStore     atomic.Uint64

	// Slot-specific lock: Key: slot number, Value: *sync.RWMutex
	slotLocks sync.Map

	txStore     TxStore
	txMetaStore TxMetaStore
	eventRouter *events.EventRouter
}

// NewGenericBlockStore creates a new generic block store with the given provider
func NewGenericBlockStore(provider db.DatabaseProvider, ts TxStore, txMetaStore TxMetaStore, eventRouter *events.EventRouter) (BlockStore, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider cannot be nil")
	}

	store := &GenericBlockStore{
		provider:    provider,
		txStore:     ts,
		eventRouter: eventRouter,
		txMetaStore: txMetaStore,
	}

	// Load existing metadata
	if err := store.loadExistingMetadataAndCleanup(); err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	// Start periodic cleanup to manage memory usage
	// Keep locks for 1000 recent slots, cleanup every 10 minutes
	exception.SafeGo("startPeriodicCleanup", func() {
		store.StartPeriodicCleanup(1000, 10*time.Minute)
	})

	return store, nil
}

// loadExistingMetadata loads the latest finalized slot and latest store slot from the database
func (s *GenericBlockStore) loadExistingMetadataAndCleanup() error {
	existLatestFinalized, err := s.loadMetaLatestFinalizedSlot()
	if err != nil {
		return fmt.Errorf("failed to get latest finalized: %w", err)
	}
	s.latestFinalized.Store(existLatestFinalized)

	existLatestStore, err := s.loadMetaLatestStoreSlot()
	if err != nil {
		return fmt.Errorf("failed to get latest store: %w", err)
	}
	if existLatestStore > existLatestFinalized {
		logx.Warn("BLOCKSTORE", fmt.Sprintf("Latest store is greater than latest finalized: %d > %d", existLatestStore, existLatestFinalized))
		removeBlockSlots := make([]uint64, 0)
		for slot := existLatestFinalized + 1; slot < existLatestStore; slot++ {
			removeBlockSlots = append(removeBlockSlots, slot)
		}
		s.removeBlockBySlots(removeBlockSlots)
		s.updateLatestStoreSlot(existLatestFinalized)
	}

	logx.Info("BLOCKSTORE", fmt.Sprintf("Loaded latest finalized slot: %d, latest store slot: %d", existLatestFinalized, existLatestStore))
	return nil
}

// slotToBlockKey converts a slot number to a block storage key
func slotToBlockKey(slot uint64) []byte {
	key := make([]byte, len(PrefixBlock)+UNINT64_BYTES)
	copy(key, PrefixBlock)
	binary.BigEndian.PutUint64(key[len(PrefixBlock):], slot)
	return key
}

// slotToFinalizedKey converts a slot number to a finalized marker key
func slotToFinalizedKey(slot uint64) []byte {
	key := make([]byte, len(PrefixBlockFinalized)+UNINT64_BYTES)
	copy(key, PrefixBlockFinalized)
	binary.BigEndian.PutUint64(key[len(PrefixBlockFinalized):], slot)
	return key
}

func metaLatestFinalizedKey() []byte {
	return []byte(PrefixBlockMeta + BlockMetaKeyLatestFinalized)
}

func metaLatestStoreKey() []byte {
	return []byte(PrefixBlockMeta + BlockMetaKeyLatestStore)
}

func (s *GenericBlockStore) loadMetaLatestFinalizedSlot() (uint64, error) {
	key := metaLatestFinalizedKey()
	value, err := s.provider.Get(key)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest finalized: %w", err)
	}
	if value == nil {
		return 0, nil
	}
	if len(value) != UNINT64_BYTES {
		return 0, fmt.Errorf("invalid latest finalized value length: %d", len(value))
	}

	return binary.BigEndian.Uint64(value), nil
}

func (s *GenericBlockStore) loadMetaLatestStoreSlot() (uint64, error) {
	key := metaLatestStoreKey()
	value, err := s.provider.Get(key)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest store: %w", err)
	}
	if value == nil {
		return 0, nil
	}
	if len(value) != UNINT64_BYTES {
		return 0, fmt.Errorf("invalid latest store value length: %d", len(value))
	}

	return binary.BigEndian.Uint64(value), nil
}

// getSlotLock returns or creates a RWMutex for the given slot
func (s *GenericBlockStore) getSlotLock(slot uint64) *sync.RWMutex {
	if lock, ok := s.slotLocks.Load(slot); ok {
		return lock.(*sync.RWMutex)
	}

	// Create new lock for this slot
	newLock := &sync.RWMutex{}
	actual, loaded := s.slotLocks.LoadOrStore(slot, newLock)
	if loaded {
		// Another goroutine created the lock first, use that one
		return actual.(*sync.RWMutex)
	}
	return newLock
}

func (s *GenericBlockStore) CleanupOldSlotLocks(keepRecentSlots uint64) {
	currentLatest := s.latestFinalized.Load()
	if currentLatest < keepRecentSlots {
		return // Not enough slots to cleanup
	}

	cleanupThreshold := currentLatest - keepRecentSlots

	// Collect slots to delete
	var slotsToDelete []uint64
	s.slotLocks.Range(func(key, value interface{}) bool {
		slotNum := key.(uint64)
		if slotNum < cleanupThreshold {
			slotsToDelete = append(slotsToDelete, slotNum)
		}
		return true // continue iteration
	})

	// Delete collected slots
	deletedCount := 0
	for _, slot := range slotsToDelete {
		s.slotLocks.Delete(slot)
		deletedCount++
	}

	if deletedCount > 0 {
		logx.Info("BLOCKSTORE", "Cleaned up", deletedCount, "slot locks older than", cleanupThreshold)
	}
}

// StartPeriodicCleanup starts a background goroutine that periodically cleans up old slot locks
func (s *GenericBlockStore) StartPeriodicCleanup(keepRecentSlots uint64, cleanupInterval time.Duration) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		exception.SafeGo("CleanupOldSlotLocks", func() {
			s.CleanupOldSlotLocks(keepRecentSlots)
		})
	}
}

// Block retrieves a block by slot number
func (s *GenericBlockStore) Block(slot uint64) *block.Block {
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
	if err := jsonx.Unmarshal(value, &blk); err != nil {
		logx.Error("BLOCKSTORE", "Failed to unmarshal block", slot, "error:", err)
		return nil
	}

	return &blk
}

// GetBatch retrieves multiple blocks by their slots using true batch operation
func (s *GenericBlockStore) GetBatch(slots []uint64) (map[uint64]*block.Block, error) {
	if len(slots) == 0 {
		logx.Info("BLOCKSTORE", "GetBatch: no slots to retrieve")
		return make(map[uint64]*block.Block), nil
	}
	logx.Info("BLOCKSTORE", fmt.Sprintf("GetBatch: retrieving %d blocks", len(slots)))

	// Prepare keys for batch operation
	keys := make([][]byte, len(slots))
	slotToKey := make(map[string]uint64, len(slots)) // Map key back to slot

	for i, slot := range slots {
		key := slotToBlockKey(slot)
		keys[i] = key
		slotToKey[string(key)] = slot
	}

	// Use true batch read - single CGO call!
	dataMap, err := s.provider.GetBatch(keys)
	if err != nil {
		logx.Error("BLOCKSTORE", fmt.Sprintf("Failed to batch get blocks: %v", err))
		return nil, fmt.Errorf("failed to batch get blocks: %w", err)
	}

	blocks := make(map[uint64]*block.Block, len(slots))

	for keyStr, data := range dataMap {
		slot := slotToKey[keyStr]

		var blk block.Block
		err = jsonx.Unmarshal(data, &blk)
		if err != nil {
			logx.Warn("BLOCKSTORE", fmt.Sprintf("Failed to unmarshal block %d: %s", slot, err.Error()))
			continue
		}

		blocks[slot] = &blk
	}

	logx.Info("BLOCKSTORE", fmt.Sprintf("GetBatch: retrieved %d/%d blocks", len(blocks), len(slots)))
	return blocks, nil
}

// HasCompleteBlock checks if a complete block exists at the given slot
func (s *GenericBlockStore) HasCompleteBlock(slot uint64) bool {
	key := slotToBlockKey(slot)
	exists, err := s.provider.Has(key)
	if err != nil {
		logx.Error("BLOCKSTORE", "Failed to check block existence", slot, "error:", err)
		return false
	}

	return exists
}

// GetLatestFinalizedSlot returns the latest finalized slot
func (s *GenericBlockStore) GetLatestFinalizedSlot() uint64 {
	return s.latestFinalized.Load()
}

// GetLatestStoreSlot returns the latest slot in the store
func (s *GenericBlockStore) GetLatestStoreSlot() uint64 {
	return s.latestStore.Load()
}

func (s *GenericBlockStore) updateLatestStoreSlot(slot uint64) error {
	s.latestStore.Store(slot)
	metaKey := metaLatestStoreKey()
	metaValue := make([]byte, UNINT64_BYTES)
	binary.BigEndian.PutUint64(metaValue, slot)
	if err := s.provider.Put(metaKey, metaValue); err != nil {
		return fmt.Errorf("failed to update latest store: %w", err)
	}
	logx.Info("BLOCKSTORE", fmt.Sprintf("Updated latest store to %d", slot))
	return nil
}

func (s *GenericBlockStore) updateLatestFinalizedSlot(slot uint64) error {
	s.latestFinalized.Store(slot)
	metaKey := metaLatestFinalizedKey()
	metaValue := make([]byte, UNINT64_BYTES)
	binary.BigEndian.PutUint64(metaValue, slot)
	if err := s.provider.Put(metaKey, metaValue); err != nil {
		return fmt.Errorf("failed to update latest finalized: %w", err)
	}
	logx.Info("BLOCKSTORE", fmt.Sprintf("Updated latest finalized to %d", slot))
	return nil
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
	logx.Info("BLOCKSTORE", fmt.Sprintf("Adding pending block at slot %d", b.Slot))

	slotLock := s.getSlotLock(b.Slot)
	slotLock.Lock()
	defer slotLock.Unlock()
	logx.Debug("BLOCKSTORE", fmt.Sprintf("Acquired lock for adding pending block at slot %d", b.Slot))

	key := slotToBlockKey(b.Slot)

	// Check if block already exists
	exists, err := s.provider.Has(key)
	if err != nil {
		return fmt.Errorf("failed to check block existence: %w", err)
	}

	if exists {
		return fmt.Errorf("block at slot %d already exists", b.Slot)
	}
	logx.Debug("BLOCKSTORE", fmt.Sprintf("OK, block does not exist at slot %d", b.Slot))

	// Store block
	value, err := jsonx.Marshal(utils.BroadcastedBlockToBlock(b))
	if err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}
	if err := s.provider.Put(key, value); err != nil {
		return fmt.Errorf("failed to store block: %w", err)
	}

	logx.Debug("BLOCKSTORE", fmt.Sprintf("Monitoring block size bytes at slot %d", b.Slot))
	monitoring.RecordBlockSizeBytes(len(value))
	logx.Info("BLOCKSTORE", fmt.Sprintf("Stored block at slot %d", b.Slot))

	// Update latest store slot if the block slot is greater than the latest store slot
	if b.Slot > s.latestStore.Load() {
		if err := s.updateLatestStoreSlot(b.Slot); err != nil {
			return fmt.Errorf("failed to update latest store: %w", err)
		}
	}

	// Store block tsx
	// TODO: storing block & its tsx should be atomic operation. Consider use batch or db transaction (if supported)
	txs := make([]*transaction.Transaction, 0)
	for _, entry := range b.Entries {
		txs = append(txs, entry.Transactions...)
	}
	monitoring.RecordTxInBlock(len(txs))
	if err := s.txStore.StoreBatch(txs); err != nil {
		return fmt.Errorf("failed to store txs: %w", err)
	}
	logx.Info("BLOCKSTORE", fmt.Sprintf("Stored txs at slot %d", b.Slot))
	// Store block txs meta
	txsMeta := make([]*types.TransactionMeta, 0)
	for _, entry := range b.Entries {
		for _, tx := range entry.Transactions {
			txsMeta = append(txsMeta, types.NewTxMeta(tx, b.Slot, b.HashString(), types.TxStatusProcessed, ""))
		}
	}
	if err := s.txMetaStore.StoreBatch(txsMeta); err != nil {
		return fmt.Errorf("failed to store txs meta: %w", err)
	}
	logx.Info("BLOCKSTORE", fmt.Sprintf("Stored txs meta at slot %d", b.Slot))
	// Publish transaction inclusion events if event router is provided
	if s.eventRouter != nil {
		blockHashHex := b.HashString()

		// Publish TransactionIncludedInBlock events for each transaction in the block
		for _, entry := range b.Entries {
			for _, tx := range entry.Transactions {
				event := events.NewTransactionIncludedInBlock(tx, b.Slot, blockHashHex)
				s.eventRouter.PublishTransactionEvent(event)
				monitoring.IncreaseExecutedTpsCount()
			}
		}
	}

	logx.Info("BLOCKSTORE", fmt.Sprintf("Added pending block at slot %d", b.Slot))

	return nil
}

// IsApplied checks if a slot has been finalized
func (s *GenericBlockStore) IsApplied(slot uint64) bool {
	// Check if this specific slot has been finalized
	key := slotToFinalizedKey(slot)
	exists, err := s.provider.Has(key)
	if err != nil {
		logx.Error("BLOCKSTORE", "Failed to check if slot is finalized", slot, "error:", err)
		return false
	}

	return exists
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

	slotLock := s.getSlotLock(slot)
	slotLock.Lock()
	defer slotLock.Unlock()

	// Publish transaction finalization events if event router is provided
	if s.eventRouter != nil && blk != nil {
		blockHashHex := blk.HashString()
		now := time.Now()

		for _, entry := range blk.Entries {
			txs, err := s.txStore.GetBatch(entry.TxHashes)
			if err != nil {
				logx.Warn("BLOCKSTORE", "Failed to get transactions for finalization event", "slot", slot, "error", err)
				continue
			}
			for _, tx := range txs {
				// Record metrics
				txTimestamp := time.UnixMilli(int64(tx.Timestamp))
				monitoring.RecordTimeToFinality(now.Sub(txTimestamp))

				event := events.NewTransactionFinalized(tx, slot, blockHashHex)
				s.eventRouter.PublishTransactionEvent(event)
				monitoring.IncreaseFinalizedTpsCount()
			}
		}
	}

	// Mark this specific slot as finalized
	finalizedKey := slotToFinalizedKey(slot)
	finalizedValue := []byte{1} // Simple marker value
	if err := s.provider.Put(finalizedKey, finalizedValue); err != nil {
		return fmt.Errorf("failed to mark slot as finalized: %w", err)
	}

	currentLatest := s.latestFinalized.Load()
	if slot > currentLatest {
		if err := s.updateLatestFinalizedSlot(slot); err != nil {
			return fmt.Errorf("failed to update latest finalized: %w", err)
		}
		// Update block height metric
		monitoring.SetBlockHeight(slot)
	}

	logx.Info("BLOCKSTORE", fmt.Sprintf("Marked block as finalized at slot %d", slot))
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
	latest := bs.latestFinalized.Load()
	if latest >= blockSlot {
		return latest - blockSlot + 1
	}
	return 1 // Confirmed but not yet finalized
}

// GetTransactionBlockInfo searches all stored blocks for a transaction. It returns the containing slot, the whole block, whether the
// block is finalized, and whether it was found.
func (bs *GenericBlockStore) GetTransactionBlockInfo(clientHashHex string) (slot uint64, blk *block.Block, finalized bool, found bool) {
	txMeta, err := bs.txMetaStore.GetByHash(clientHashHex)
	if err != nil {
		return 0, nil, false, false
	}
	if txMeta.Status != types.TxStatusProcessed {
		return 0, nil, false, false
	}

	blk = bs.Block(txMeta.Slot)
	if blk == nil {
		return 0, nil, false, false
	}

	return txMeta.Slot, blk, blk.Status == block.BlockFinalized, true
}

func (bs *GenericBlockStore) removeBlockBySlots(slots []uint64) error {
	if len(slots) == 0 {
		logx.Debug("BLOCKSTORE", "removeBlockBySlots: no slots provided")
		return nil
	}

	logx.Info("BLOCKSTORE", fmt.Sprintf("removeBlockBySlots: start removing %d slots", len(slots)))

	// 1) Load blocks in batch to collect all tx hashes
	blocks, err := bs.GetBatch(slots)
	if err != nil {
		logx.Error("BLOCKSTORE", fmt.Sprintf("removeBlockBySlots: failed to batch load blocks: %v", err))
		return fmt.Errorf("failed to load blocks for deletion: %w", err)
	}

	txHashesSet := make(map[string]struct{})
	for _, slot := range slots {
		blk, ok := blocks[slot]
		if !ok || blk == nil {
			logx.Warn("BLOCKSTORE", fmt.Sprintf("removeBlockBySlots: block at slot %d not found during deletion", slot))
			continue
		}
		for _, entry := range blk.Entries {
			for _, h := range entry.TxHashes {
				txHashesSet[h] = struct{}{}
			}
		}
	}

	// Convert set to slice
	txHashes := make([]string, 0, len(txHashesSet))
	for h := range txHashesSet {
		txHashes = append(txHashes, h)
	}

	logx.Info("BLOCKSTORE", fmt.Sprintf("removeBlockBySlots: collected %d unique tx hashes to delete", len(txHashes)))

	// 2) Delete tx metas then txs in batches
	if len(txHashes) > 0 {
		if err := bs.txMetaStore.DeleteBatch(txHashes); err != nil {
			logx.Error("BLOCKSTORE", fmt.Sprintf("removeBlockBySlots: failed to delete tx metas: %v", err))
			return fmt.Errorf("failed to delete tx metas: %w", err)
		}
		if err := bs.txStore.DeleteBatch(txHashes); err != nil {
			logx.Error("BLOCKSTORE", fmt.Sprintf("removeBlockBySlots: failed to delete txs: %v", err))
			return fmt.Errorf("failed to delete txs: %w", err)
		}
	}

	// 3) Delete blocks in a single db batch
	batch := bs.provider.Batch()
	defer batch.Close()

	for _, slot := range slots {
		key := slotToBlockKey(slot)
		batch.Delete(key)
	}

	if err := batch.Write(); err != nil {
		logx.Error("BLOCKSTORE", fmt.Sprintf("removeBlockBySlots: failed to batch remove blocks: %v", err))
		return fmt.Errorf("failed to batch remove blocks: %w", err)
	}

	logx.Info("BLOCKSTORE", fmt.Sprintf("removeBlockBySlots: removed %d blocks and %d txs/metas", len(slots), len(txHashes)))
	return nil
}
