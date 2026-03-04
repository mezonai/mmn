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
	GetSlot(slot uint64) (*block.SlotInfo, error)
	GetSlotsByHeights(heights []uint64) (map[uint64]uint64, error) // Returns map of height -> slot
	HasCompleteBlock(slot uint64) bool
	LastEntryInfoAtSlot(slot uint64) (SlotBoundary, bool)
	GetLatestStoreSlot() uint64
	GetLatestHeight() uint64 // New method to get latest sequential height
	AddBlockPending(b *block.BroadcastedBlock) error
	FinalizeBlock(blk *block.Block, txMetas map[string]*types.TransactionMeta, addrAccount map[string]*types.Account, latestVersionContentHashMap map[string]string, optSlot ...uint64) error
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
	latestHeight    atomic.Uint64

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

func (s *GenericBlockStore) loadLatestFinalized() error {
	key := []byte(PrefixBlockMeta + BlockMetaKeyLatestFinalized)
	value, err := s.provider.Get(key)
	if err != nil {
		return fmt.Errorf("failed to get latest finalized: %w", err)
	}

	if value == nil {
		// No existing data, start from 0
		s.latestFinalized.Store(0)
	} else if len(value) != 8 {
		return fmt.Errorf("invalid latest finalized value length: %d", len(value))
	} else {
		s.latestFinalized.Store(binary.BigEndian.Uint64(value))
	}

	key = []byte(PrefixBlockMeta + BlockMetaKeyLatestStore)
	value, err = s.provider.Get(key)
	if err != nil {
		return fmt.Errorf("failed to get latest store: %w", err)
	}

	if value == nil {
		s.latestStore.Store(0)
	} else if len(value) != 8 {
		return fmt.Errorf("invalid latest store value length: %d", len(value))
	} else {
		s.latestStore.Store(binary.BigEndian.Uint64(value))
	}

	key = []byte(PrefixBlockMeta + BlockMetaKeyLatestHeight)
	value, err = s.provider.Get(key)
	if err != nil {
		return fmt.Errorf("failed to get latest height: %w", err)
	}

	if value == nil {
		s.latestHeight.Store(0)
	} else if len(value) != 8 {
		return fmt.Errorf("invalid latest height value length: %d", len(value))
	} else {
		s.latestHeight.Store(binary.BigEndian.Uint64(value))
	}

	return nil
}

// slotToBlockKey converts a slot number to a block storage key
func slotToBlockKey(slot uint64) []byte {
	key := make([]byte, len(PrefixBlock)+8)
	copy(key, PrefixBlock)
	binary.BigEndian.PutUint64(key[len(PrefixBlock):], slot)
	return key
}

// slotToSlotKey converts a slot number to a slot storage key
func slotToSlotKey(slot uint64) []byte {
	key := make([]byte, len(PrefixSlot)+8)
	copy(key, PrefixSlot)
	binary.BigEndian.PutUint64(key[len(PrefixSlot):], slot)
	return key
}

// heightToSlotKey converts a height to a height-to-slot mapping key
func heightToSlotKey(height uint64) []byte {
	key := make([]byte, len(PrefixHeightToSlot)+8)
	copy(key, PrefixHeightToSlot)
	binary.BigEndian.PutUint64(key[len(PrefixHeightToSlot):], height)
	return key
}

// slotToFinalizedKey converts a slot number to a finalized marker key
func slotToFinalizedKey(slot uint64) []byte {
	key := make([]byte, len(PrefixSlotFinalized)+8)
	copy(key, PrefixSlotFinalized)
	binary.BigEndian.PutUint64(key[len(PrefixSlotFinalized):], slot)
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

// GetLatestStoreSlot returns the latest slot in the store
func (s *GenericBlockStore) GetLatestStoreSlot() uint64 {
	return s.latestStore.Load()
}

// GetLatestHeight returns the latest sequential block height
func (s *GenericBlockStore) GetLatestHeight() uint64 {
	return s.latestHeight.Load()
}

// GetSlot retrieves a slot by slot number
func (s *GenericBlockStore) GetSlot(slot uint64) (*block.SlotInfo, error) {
	key := slotToSlotKey(slot)
	value, err := s.provider.Get(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get slot %d: %w", slot, err)
	}

	if value == nil {
		return nil, nil // Slot does not exist
	}

	var slotInfo block.SlotInfo
	if err := jsonx.Unmarshal(value, &slotInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal slot %d: %w", slot, err)
	}

	return &slotInfo, nil
}

// GetSlotsByHeights retrieves a batch mapping of heights to slots
func (s *GenericBlockStore) GetSlotsByHeights(heights []uint64) (map[uint64]uint64, error) {
	if len(heights) == 0 {
		return make(map[uint64]uint64), nil
	}

	keys := make([][]byte, len(heights))
	heightToKey := make(map[string]uint64, len(heights))

	for i, height := range heights {
		key := heightToSlotKey(height)
		keys[i] = key
		heightToKey[string(key)] = height
	}

	dataMap, err := s.provider.GetBatch(keys)
	if err != nil {
		return nil, fmt.Errorf("failed to batch get height mappings: %w", err)
	}

	heightToSlot := make(map[uint64]uint64, len(heights))
	for keyStr, data := range dataMap {
		height := heightToKey[keyStr]
		if len(data) != 8 {
			logx.Warn("BLOCKSTORE", fmt.Sprintf("Invalid mapping value length for height %d", height))
			continue
		}
		heightToSlot[height] = binary.BigEndian.Uint64(data)
	}

	return heightToSlot, nil
}

// LastEntryInfoAtSlot returns the slot boundary information for the given slot
func (s *GenericBlockStore) LastEntryInfoAtSlot(slot uint64) (SlotBoundary, bool) {
	// First check SlotStore object since full Block might not be stored (empty block)
	slotInfo, err := s.GetSlot(slot)
	if err != nil || slotInfo == nil {
		return SlotBoundary{}, false
	}

	return SlotBoundary{
		Slot: slot,
		Hash: slotInfo.LastEntryHash,
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

	// Determine if block has any non-tick transactions
	hasTxs := false
	for _, entry := range b.Entries {
		if entry.Tick {
			continue
		}
		if len(entry.Transactions) > 0 {
			hasTxs = true
			break
		}
	}

	// Always create and save SlotInfo
	slotKey := slotToSlotKey(slot)
	exists, err := s.provider.Has(slotKey)
	if err != nil {
		return fmt.Errorf("failed to check slot existence: %w", err)
	}

	if exists {
		return fmt.Errorf("block at slot %d already exists", slot)
	}

	slotInfo := &block.SlotInfo{
		Slot:          slot,
		PrevHash:      b.PrevHash,
		LastEntryHash: b.LastEntryHash(), // newly added field
		LeaderID:      b.LeaderID,
		Timestamp:     b.Timestamp,
	}

	// Get batch from provider
	batch := s.provider.Batch()
	defer batch.Close()

	slotData, err := jsonx.Marshal(slotInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal slot info: %w", err)
	}
	batch.Put(slotKey, slotData)

	key := slotToBlockKey(slot)

	if hasTxs {
		// Calculate the next height
		nextHeight := s.latestHeight.Load() + 1
		b.Height = nextHeight

		// Store block
		bBlock := utils.BroadcastedBlockToBlock(b)
		value, err := jsonx.Marshal(bBlock)
		if err != nil {
			return fmt.Errorf("failed to marshal block: %w", err)
		}
		batch.Put(key, value)

		// Map height -> slot
		hKey := heightToSlotKey(nextHeight)
		hValue := make([]byte, 8)
		binary.BigEndian.PutUint64(hValue, slot)
		batch.Put(hKey, hValue)

		// Update latest height (must use nextHeight, NOT slot/hValue)
		metaHeightKey := []byte(PrefixBlockMeta + BlockMetaKeyLatestHeight)
		metaHeightValue := make([]byte, 8)
		binary.BigEndian.PutUint64(metaHeightValue, nextHeight)
		batch.Put(metaHeightKey, metaHeightValue)
	}

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
	if hasTxs {
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
	}

	// Batch write all changes
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to batch write to database: %w", err)
	}

	if hasTxs {
		s.latestHeight.Store(b.Height)
		logx.Info("BLOCKSTORE", fmt.Sprintf("Batch stored block (height %d), txs, txs meta at slot %d", b.Height, slot))

		logx.Debug("BLOCKSTORE", fmt.Sprintf("Monitoring block bytes at slot %d", slot)) // value not accessible here, omitting size check, can just log
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
	} else {
		logx.Info("BLOCKSTORE", fmt.Sprintf("Skipping save block at slot %d (empty). Only slot info saved.", slot))
	}

	logx.Info("BLOCKSTORE", fmt.Sprintf("Added pending block processing at slot %d", slot))

	return nil
}

// IsApplied checks if a slot has been finalized (not just pending)
func (s *GenericBlockStore) IsApplied(slot uint64) bool {
	// Check finalized marker key, NOT slot key.
	// Slot key is written by AddBlockPending, so checking it would always return true
	// and prevent FinalizeBlock from ever being called (causing txMeta to stay CONFIRMED).
	// The finalized marker is only written by FinalizeBlock — this is the correct check.
	key := slotToFinalizedKey(slot)
	exists, err := s.provider.Has(key)
	if err != nil {
		logx.Error("BLOCKSTORE", "Failed to check if slot is applied", slot, "error:", err)
		return false
	}

	return exists
}

// FinalizeBlock stores transaction metas and account states, marking the block as finalized
func (s *GenericBlockStore) FinalizeBlock(blk *block.Block, txMetas map[string]*types.TransactionMeta, addrAccount map[string]*types.Account, latestVersionContentHashMap map[string]string, optSlot ...uint64) error {
	var slot uint64
	if blk != nil {
		slot = blk.Slot
	} else if len(optSlot) > 0 {
		slot = optSlot[0]
	} else {
		return fmt.Errorf("block is nil and slot is not provided")
	}

	slotLock := s.getSlotLock(slot)
	slotLock.Lock()
	defer slotLock.Unlock()

	batch := s.provider.Batch()
	defer batch.Close()

	if s.HasCompleteBlock(slot) {
		// Mark block as finalized only if it exists
		blk.Status = block.BlockFinalized
		blkKey := slotToBlockKey(slot)
		blkValue, err := jsonx.Marshal(blk)
		if err != nil {
			return fmt.Errorf("failed to marshal block: %w", err)
		}
		batch.Put(blkKey, blkValue)
	}

	// Always update tx metas regardless of block existence.
	// This is critical for listener nodes where the block key may not be stored
	// (only slot key is saved), but txMeta must still transition to FINALIZED status.
	for _, txMeta := range txMetas {
		data, err := jsonx.Marshal(txMeta)
		if err != nil {
			return fmt.Errorf("failed to marshal transaction meta: %w", err)
		}
		batch.Put(s.txMetaStore.GetDBKey(txMeta.TxHash), data)
	}

	// Always update account states
	for _, account := range addrAccount {
		accountData, err := jsonx.Marshal(account)
		if err != nil {
			return fmt.Errorf("failed to marshal account: %w", err)
		}
		batch.Put(s.accStore.GetDBKey(account.Address), accountData)
	}

	// Always update latest version content hashes
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
	if slot > currentLatest {
		s.latestFinalized.Store(slot)
	}
	// Update block height metric
	monitoring.SetBlockHeight(slot) // Note: this is actually slot max, not height, but maintaining existing functionality

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
