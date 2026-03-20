package cmd

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/store"
	"github.com/spf13/cobra"
)

var (
	migrateDataDir     string
	migrateBackendType string
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate MMN database to the new schema with Block Height, SlotInfo, and finalized slot marker",
	RunE:  runMigration,
}

func init() {
	rootCmd.AddCommand(migrateCmd)

	migrateCmd.Flags().StringVar(&migrateDataDir, "data-dir", ".", "Directory containing node data (store database should be in store subfolder)")
	migrateCmd.Flags().StringVar(&migrateBackendType, "database", "leveldb", "Database backend to open (leveldb or rocksdb)")
}

func runMigration(cmd *cobra.Command, args []string) error {
	log.Printf("Starting migration on data-dir: %s with backend: %s\n", migrateDataDir, migrateBackendType)

	storeDir := migrateDataDir + "/store"
	if _, err := os.Stat(storeDir); os.IsNotExist(err) {
		return fmt.Errorf("store directory not found at: %s", storeDir)
	}

	factory := store.NewStoreFactory()
	provider, err := factory.CreateProvider(&store.StoreConfig{
		Type:      store.StoreType(migrateBackendType),
		Directory: storeDir,
	})
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer provider.Close()

	iterProvider, ok := provider.(db.IterableProvider)
	if !ok {
		return fmt.Errorf("database provider does not support iteration")
	}

	batch := provider.Batch()
	defer batch.Close()

	var currentHeight uint64 = 0
	var processedBlocks, maxSlot uint64 = 0, 0
	var batchOpsCount = 0

	const batchLimit = 100000
	const logInterval = 50000

	log.Println("--- Phase 1: Migrate Blocks (blk:) & Create SlotInfo (slot: & height_to_slot:) ---")

	err = iterProvider.IteratePrefix([]byte(store.PrefixBlock), func(key, value []byte) bool {
		// filter by key length, because "blk:" prefix also matches "blk_meta:" and "blk_finalized:"
		if len(key) != len(store.PrefixBlock)+8 {
			return true
		}

		var blk block.Block
		if err := jsonx.Unmarshal(value, &blk); err != nil {
			log.Printf("Warning: failed to unmarshal block at key %s: %v\n", string(key), err)
			return true // Continue to next
		}

		if blk.Slot > maxSlot {
			maxSlot = blk.Slot
		}

		// Detect if block has non-tick transactions
		hasTxs := false
		for _, entry := range blk.Entries {
			if entry.Tick {
				continue
			}
			if len(entry.TxHashes) > 0 {
				hasTxs = true
				break
			}
		}

		// Calculate height and map height if it has txs
		if hasTxs {
			currentHeight++
			blk.Height = currentHeight

			// Overwrite blk: with new Height
			newBlkBytes, err := jsonx.Marshal(blk)
			if err != nil {
				log.Printf("Warning: failed to marshal updated block for slot %d: %v\n", blk.Slot, err)
			} else {
				batch.Put(key, newBlkBytes)
				batchOpsCount++
			}

			// Map height_to_slot
			hKey := make([]byte, len(store.PrefixHeightToSlot)+8)
			copy(hKey, store.PrefixHeightToSlot)
			binary.BigEndian.PutUint64(hKey[len(store.PrefixHeightToSlot):], currentHeight)

			hVal := make([]byte, 8)
			binary.BigEndian.PutUint64(hVal, blk.Slot)

			batch.Put(hKey, hVal)
			batchOpsCount++
		}

		// Create slot:
		slotInfo := &block.SlotInfo{
			Slot:          blk.Slot,
			PrevHash:      blk.PrevHash,
			LastEntryHash: blk.LastEntryHash(), // returns [32]byte{} if no entries, exactly as needed
			LeaderID:      blk.LeaderID,
			Timestamp:     blk.Timestamp,
		}

		slotKey := make([]byte, len(store.PrefixSlot)+8)
		copy(slotKey, store.PrefixSlot)
		binary.BigEndian.PutUint64(slotKey[len(store.PrefixSlot):], blk.Slot)

		slotBytes, err := jsonx.Marshal(slotInfo)
		if err == nil {
			batch.Put(slotKey, slotBytes)
			batchOpsCount++
		}

		processedBlocks++
		if processedBlocks%uint64(logInterval) == 0 {
			log.Printf("Scanned %d blocks, current max height is %d", processedBlocks, currentHeight)
		}

		if batchOpsCount >= batchLimit {
			if err := batch.Write(); err != nil {
				log.Printf("Error writing batch during block iteration: %v", err)
			}
			batch.Reset()
			batchOpsCount = 0
		}

		return true // continue iteration
	})

	if err != nil {
		return fmt.Errorf("iteration error: %w", err)
	}

	// Update latest block height
	metaHeightKey := []byte(store.PrefixBlockMeta + store.BlockMetaKeyLatestHeight)
	metaHeightValue := make([]byte, 8)
	binary.BigEndian.PutUint64(metaHeightValue, currentHeight)
	batch.Put(metaHeightKey, metaHeightValue)
	batchOpsCount++

	if batchOpsCount > 0 {
		if err := batch.Write(); err != nil {
			return fmt.Errorf("error writing final block iteration batch: %w", err)
		}
		batch.Reset()
		batchOpsCount = 0
	}

	log.Printf("Phase 1 completed. Total blocks scanned: %d, Latest Height: %d\n", processedBlocks, currentHeight)
	log.Println("--- Phase 2: Migrate Finalized Slots (blk_finalized: to slot_finalized:) ---")

	var processedFinalized uint64 = 0
	// old prefix wasn't in constant, assuming "blk_finalized:"
	err = iterProvider.IteratePrefix([]byte("blk_finalized:"), func(key, value []byte) bool {
		// key is "blk_finalized:{slot_bytes}"
		if len(key) != len("blk_finalized:")+8 {
			return true // Invalid format, ignore
		}

		slot := binary.BigEndian.Uint64(key[len("blk_finalized:"):])

		// create new slot_finalized key
		newKey := make([]byte, len(store.PrefixSlotFinalized)+8)
		copy(newKey, store.PrefixSlotFinalized)
		binary.BigEndian.PutUint64(newKey[len(store.PrefixSlotFinalized):], slot)

		batch.Put(newKey, []byte{1})
		batch.Delete(key)

		batchOpsCount += 2
		processedFinalized++

		if processedFinalized%uint64(logInterval) == 0 {
			log.Printf("Processed %d finalized markers", processedFinalized)
		}

		if batchOpsCount >= batchLimit {
			if err := batch.Write(); err != nil {
				log.Printf("Error writing batch during finalization iteration: %v", err)
			}
			batch.Reset()
			batchOpsCount = 0
		}

		return true
	})

	if err != nil {
		return fmt.Errorf("finalization iteration error: %w", err)
	}

	if batchOpsCount > 0 {
		if err := batch.Write(); err != nil {
			return fmt.Errorf("error writing final finalization batch: %w", err)
		}
	}

	log.Printf("Phase 2 completed. Total finalized markers migrated: %d\n", processedFinalized)
	log.Println("=== Migration Finished Successfully ===")

	return nil
}
