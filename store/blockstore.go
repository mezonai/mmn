package store

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"sync/atomic"

	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/monitoring"
	"github.com/mezonai/mmn/types"

	"github.com/mezonai/mmn/utils"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
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
	FinalizeBlock(blk *block.Block, txMetas map[string]*types.TransactionMeta, addrAccount map[string]*types.Account, latestVersionContentHashMap map[string]string) error
	GetConfirmations(blockSlot uint64) uint64
	MustClose()
	IsApplied(slot uint64) bool
	ForceAddBlockPendingOnly(b *block.BroadcastedBlock) error
	ForceMarkBlockAsFinalized(blk *block.Block) error
	RemoveBlocks(slots []uint64) error
}

// GenericBlockStore is a database-agnostic implementation that uses DatabaseProvider
// This allows it to work with any database backend (LevelDB, RocksDB, etc.)
type GenericBlockStore struct {
	provider db.DatabaseProvider

	latestFinalized atomic.Uint64
	latestStore     atomic.Uint64

	// Slot-specific lock: Key: slot number, Value: *sync.RWMutex
	slotLocks sync.Map

	accStore    AccountStore
	txStore     TxStore
	txMetaStore TxMetaStore
	eventRouter *events.EventRouter
}

// NewGenericBlockStore creates a new generic block store with the given provider
func NewGenericBlockStore(provider db.DatabaseProvider, ts TxStore, txMetaStore TxMetaStore, accStore AccountStore, eventRouter *events.EventRouter) (BlockStore, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider cannot be nil")
	}

	store := &GenericBlockStore{
		provider:    provider,
		txStore:     ts,
		eventRouter: eventRouter,
		txMetaStore: txMetaStore,
		accStore:    accStore,
	}

	// Load existing metadata
	if err := store.loadLatestFinalized(); err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	// Start periodic cleanup to manage memory usage
	// Keep locks for 1000 recent slots, cleanup every 10 minutes
	exception.SafeGo("startPeriodicCleanup", func() {
		store.StartPeriodicCleanup(1000, 10*time.Minute)
	})

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
		s.latestFinalized.Store(0)
		return nil
	}

	if len(value) != 8 {
		return fmt.Errorf("invalid latest finalized value length: %d", len(value))
	}

	s.latestFinalized.Store(binary.BigEndian.Uint64(value))

	key = []byte(PrefixBlockMeta + BlockMetaKeyLatestStore)
	value, err = s.provider.Get(key)
	if err != nil {
		return fmt.Errorf("failed to get latest store: %w", err)
	}

	if value == nil {
		s.latestStore.Store(0)
		return nil
	}

	if len(value) != 8 {
		return fmt.Errorf("invalid latest store value length: %d", len(value))
	}

	s.latestStore.Store(binary.BigEndian.Uint64(value))
	return nil
}

// slotToBlockKey converts a slot number to a block storage key
func slotToBlockKey(slot uint64) []byte {
	key := make([]byte, len(PrefixBlock)+8)
	copy(key, PrefixBlock)
	binary.BigEndian.PutUint64(key[len(PrefixBlock):], slot)
	return key
}

// slotToFinalizedKey converts a slot number to a finalized marker key
func slotToFinalizedKey(slot uint64) []byte {
	key := make([]byte, len(PrefixBlockFinalized)+8)
	copy(key, PrefixBlockFinalized)
	binary.BigEndian.PutUint64(key[len(PrefixBlockFinalized):], slot)
	return key
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
		return true
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
	slot := b.Slot
	logx.Info("BLOCKSTORE", fmt.Sprintf("Adding pending block at slot %d", slot))

	slotLock := s.getSlotLock(slot)
	slotLock.Lock()
	defer slotLock.Unlock()
	logx.Debug("BLOCKSTORE", fmt.Sprintf("Acquired lock for adding pending block at slot %d", slot))

	key := slotToBlockKey(slot)

	// Check if block already exists
	exists, err := s.provider.Has(key)
	if err != nil {
		return fmt.Errorf("failed to check block existence: %w", err)
	}

	if exists {
		return fmt.Errorf("block at slot %d already exists", slot)
	}
	logx.Debug("BLOCKSTORE", fmt.Sprintf("OK, block does not exist at slot %d", slot))

	// Get batch from provider
	batch := s.provider.Batch()
	defer batch.Close()

	// Store block
	value, err := jsonx.Marshal(utils.BroadcastedBlockToBlock(b))
	if err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}
	batch.Put(key, value)

	// Update latest store slot if the block slot is greater than the latest store slot
	if slot > s.latestStore.Load() {
		s.latestStore.Store(slot)
		metaKey := []byte(PrefixBlockMeta + BlockMetaKeyLatestStore)
		metaValue := make([]byte, 8)
		binary.BigEndian.PutUint64(metaValue, slot)
		batch.Put(metaKey, metaValue)
	}

	count := 0
	// Store transactions and transaction metas
	for _, entry := range b.Entries {
		if entry.Tick {
			continue
		}
		for _, tx := range entry.Transactions {
			// Store block transaction
			txData, err := jsonx.Marshal(tx)
			if err != nil {
				return fmt.Errorf("failed to marshal transaction: %w", err)
			}
			batch.Put(s.txStore.GetDBKey(tx.Hash()), txData)

			// Store block transaction meta
			txMeta := types.NewTxMeta(tx, slot, b.HashString(), types.TxStatusProcessed, "")
			data, err := jsonx.Marshal(txMeta)
			if err != nil {
				return fmt.Errorf("failed to marshal transaction meta: %w", err)
			}
			batch.Put(s.txMetaStore.GetDBKey(tx.Hash()), data)
		}
		count += len(entry.Transactions)
	}

	// Batch write all changes
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to batch write to database: %w", err)
	}
	logx.Info("BLOCKSTORE", fmt.Sprintf("Batch stored block, txs, txs meta at slot %d", slot))

	logx.Debug("BLOCKSTORE", fmt.Sprintf("Monitoring block size bytes at slot %d", slot))
	monitoring.RecordBlockSizeBytes(len(value))
	monitoring.RecordTxInBlock(count)

	// Publish transaction inclusion events if event router is provided
	if s.eventRouter != nil {
		blockHashHex := b.HashString()

		// Publish TransactionIncludedInBlock events for each transaction in the block
		for _, entry := range b.Entries {
			if entry.Tick {
				continue
			}
			for _, tx := range entry.Transactions {
				event := events.NewTransactionIncludedInBlock(tx, slot, blockHashHex)
				s.eventRouter.PublishTransactionEvent(event)
				monitoring.IncreaseExecutedTpsCount()
			}
		}
	}

	logx.Info("BLOCKSTORE", fmt.Sprintf("Added pending block at slot %d", slot))

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

// FinalizeBlock stores transaction metas and account states, marking the block as finalized
func (s *GenericBlockStore) FinalizeBlock(blk *block.Block, txMetas map[string]*types.TransactionMeta, addrAccount map[string]*types.Account, latestVersionContentHashMap map[string]string) error {
	slot := blk.Slot
	if !s.HasCompleteBlock(slot) {
		return fmt.Errorf("block at slot %d does not exist", slot)
	}

	slotLock := s.getSlotLock(slot)
	slotLock.Lock()
	defer slotLock.Unlock()

	batch := s.provider.Batch()
	defer batch.Close()

	// Mark block as finalized
	blk.Status = block.BlockFinalized
	blkKey := slotToBlockKey(slot)
	blkValue, err := jsonx.Marshal(blk)
	if err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}
	batch.Put(blkKey, blkValue)

	// Store batch of tx metas
	for _, txMeta := range txMetas {
		data, err := jsonx.Marshal(txMeta)
		if err != nil {
			return fmt.Errorf("failed to marshal transaction meta: %w", err)
		}
		batch.Put(s.txMetaStore.GetDBKey(txMeta.TxHash), data)
	}

	// Store batch of accounts
	for _, account := range addrAccount {
		accountData, err := jsonx.Marshal(account)
		if err != nil {
			return fmt.Errorf("failed to marshal account: %w", err)
		}
		batch.Put(s.accStore.GetDBKey(account.Address), accountData)
	}

	// Store latest version contents
	for rootHash, txHash := range latestVersionContentHashMap {
		data, err := hex.DecodeString(txHash)
		if err != nil {
			return fmt.Errorf("failed to decode txHash %s: %w", txHash, err)
		}
		batch.Put(s.txStore.GetLatestVersionContentKey(rootHash), data)
	}

	// Mark this specific slot as finalized
	finalizedKey := slotToFinalizedKey(slot)
	finalizedValue := []byte{1} // Simple marker value
	batch.Put(finalizedKey, finalizedValue)

	currentLatest := s.latestFinalized.Load()
	if slot > currentLatest {
		metaKey := []byte(PrefixBlockMeta + BlockMetaKeyLatestFinalized)
		metaValue := make([]byte, 8)
		binary.BigEndian.PutUint64(metaValue, slot)
		batch.Put(metaKey, metaValue)
	}

	// Batch write all changes
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch: %w", err)
	}
	s.latestFinalized.Store(slot)
	// Update block height metric
	monitoring.SetBlockHeight(slot)

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
func (s *GenericBlockStore) GetConfirmations(blockSlot uint64) uint64 {
	latest := s.latestFinalized.Load()
	if latest >= blockSlot {
		return latest - blockSlot + 1
	}
	return 1 // Confirmed but not yet finalized
}

// ForceAddBlockPendingOnly adds a pending block to the store, only use for truncate cmd
func (s *GenericBlockStore) ForceAddBlockPendingOnly(b *block.BroadcastedBlock) error {
	if b == nil {
		return fmt.Errorf("block cannot be nil")
	}
	slot := b.Slot
	logx.Info("BLOCKSTORE", fmt.Sprintf("ForceAddBlockPendingOnly: adding pending block at slot %d", slot))

	batch := s.provider.Batch()
	defer batch.Close()

	key := slotToBlockKey(slot)
	// Store block
	value, err := jsonx.Marshal(utils.BroadcastedBlockToBlock(b))
	if err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}
	batch.Put(key, value)

	metaKey := []byte(PrefixBlockMeta + BlockMetaKeyLatestStore)
	metaValue := make([]byte, 8)
	binary.BigEndian.PutUint64(metaValue, slot)
	batch.Put(metaKey, metaValue)

	// Batch write all changes
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to batch write to database: %w", err)
	}
	logx.Info("BLOCKSTORE", fmt.Sprintf("ForceAddBlockPendingOnly: added pending block at slot %d", slot))
	return nil
}

// ForceMarkBlockAsFinalized marks a block as finalized, only use for truncate cmd
func (s *GenericBlockStore) ForceMarkBlockAsFinalized(blk *block.Block) error {
	batch := s.provider.Batch()
	defer batch.Close()

	// Mark block as finalized
	blk.Status = block.BlockFinalized
	blkKey := slotToBlockKey(blk.Slot)
	blkValue, err := jsonx.Marshal(blk)
	if err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}
	batch.Put(blkKey, blkValue)

	// Mark this specific slot as finalized
	finalizedKey := slotToFinalizedKey(blk.Slot)
	finalizedValue := []byte{1} // Simple marker value
	batch.Put(finalizedKey, finalizedValue)

	// Force update latest finalized slot meta data
	metaKey := []byte(PrefixBlockMeta + BlockMetaKeyLatestFinalized)
	metaValue := make([]byte, 8)
	binary.BigEndian.PutUint64(metaValue, blk.Slot)
	batch.Put(metaKey, metaValue)

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch: %w", err)
	}

	logx.Info("BLOCKSTORE", fmt.Sprintf("ForceMarkBlockAsFinalized: marked block at slot %d as finalized", blk.Slot))
	return nil
}

// RemoveBlocks removes blocks from the store, only use for truncate cmd
func (s *GenericBlockStore) RemoveBlocks(slots []uint64) error {
	if len(slots) == 0 {
		logx.Info("BLOCKSTORE", "RemoveBlocks: no slots provided")
		return nil
	}

	batch := s.provider.Batch()
	defer batch.Close()

	for _, slot := range slots {
		key := slotToBlockKey(slot)
		batch.Delete(key)
	}

	if err := batch.Write(); err != nil {
		logx.Error("BLOCKSTORE", fmt.Sprintf("RemoveBlocks: failed to batch remove blocks: %v", err))
		return fmt.Errorf("failed to batch remove blocks: %w", err)
	}

	logx.Info("BLOCKSTORE", fmt.Sprintf("RemoveBlocks: removed %d blocks", len(slots)))

	return nil
}
