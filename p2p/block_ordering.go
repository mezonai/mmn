package p2p

import (
	"fmt"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/store"
)

func (ln *Libp2pNetwork) AddBlockToOrderingQueue(blk *block.BroadcastedBlock, bs store.BlockStore) error {
	if blk == nil {
		return fmt.Errorf("block is nil")
	}

	ln.blockOrderingMu.Lock()
	defer ln.blockOrderingMu.Unlock()

	// Skip if block already exists in store
	if existingBlock := bs.Block(blk.Slot); existingBlock != nil {
		logx.Info("BLOCK:ORDERING", "Block at slot", blk.Slot, "already exists, skipping")
		return nil
	}

	// Add block to queue
	ln.blockOrderingQueue[blk.Slot] = blk
	logx.Info("BLOCK:ORDERING", "Added block to queue at slot", blk.Slot, "queue size:", len(ln.blockOrderingQueue), "nextExpectedSlot:", ln.nextExpectedSlot)

	return ln.processConsecutiveBlocks(bs)
}

func (ln *Libp2pNetwork) processConsecutiveBlocks(bs store.BlockStore) error {
	var processedBlocks []*block.BroadcastedBlock

	// Find consecutive blocks starting from nextExpectedSlot
	for {
		if blk, exists := ln.blockOrderingQueue[ln.nextExpectedSlot]; exists {
			processedBlocks = append(processedBlocks, blk)
			delete(ln.blockOrderingQueue, ln.nextExpectedSlot)
			ln.nextExpectedSlot++
		} else {
			break
		}
	}

	if len(processedBlocks) == 0 {
		if len(ln.blockOrderingQueue) > 0 {
			var lowestSlot uint64
			first := true
			for slot := range ln.blockOrderingQueue {
				if first || slot < lowestSlot {
					lowestSlot = slot
					first = false
				}
			}

			// If there's a large gap (> BatchSize slots), adjust nextExpectedSlot
			if lowestSlot > ln.nextExpectedSlot+SyncBlocksBatchSize {
				logx.Warn("BLOCK:ORDERING", "Large gap detected! Adjusting nextExpectedSlot from", ln.nextExpectedSlot, "to", lowestSlot)
				ln.nextExpectedSlot = lowestSlot
				// Try processing again with adjusted slot
				return ln.processConsecutiveBlocks(bs)
			}
		}
	}

	// Process all consecutive blocks
	for _, blk := range processedBlocks {
		if err := ln.processBlock(blk, bs); err != nil {
			logx.Error("BLOCK:ORDERING", "Failed to process block at slot", blk.Slot, "error:", err)
			// Continue processing other blocks even if one fails
			continue
		}
	}

	if len(processedBlocks) > 0 {
		logx.Info("BLOCK:ORDERING", "Processed", len(processedBlocks), "consecutive blocks, next expected slot:", ln.nextExpectedSlot)
	}

	return nil
}

func (ln *Libp2pNetwork) processBlock(blk *block.BroadcastedBlock, bs store.BlockStore) error {
	// Verify PoH
	if err := blk.VerifyPoH(); err != nil {
		return fmt.Errorf("invalid PoH for block at slot %d: %w", blk.Slot, err)
	}

	// Add to block store
	if err := bs.AddBlockPending(blk); err != nil {
		return fmt.Errorf("failed to store block at slot %d: %w", blk.Slot, err)
	}

	// Mark as finalized
	if err := bs.MarkFinalized(blk.Slot); err != nil {
		return fmt.Errorf("failed to finalize block at slot %d: %w", blk.Slot, err)
	}

	// Remove from missing blocks tracker
	ln.removeFromMissingTracker(blk.Slot)

	logx.Info("BLOCK:ORDERING", "Successfully processed block at slot", blk.Slot)
	return nil
}

func (ln *Libp2pNetwork) GetOrderingQueueStatus() (nextExpected uint64, queueSize int, queuedSlots []uint64) {
	ln.blockOrderingMu.RLock()
	defer ln.blockOrderingMu.RUnlock()

	nextExpected = ln.nextExpectedSlot
	queueSize = len(ln.blockOrderingQueue)

	queuedSlots = make([]uint64, 0, len(ln.blockOrderingQueue))
	for slot := range ln.blockOrderingQueue {
		queuedSlots = append(queuedSlots, slot)
	}

	return nextExpected, queueSize, queuedSlots
}

func (ln *Libp2pNetwork) SetNextExpectedSlot(slot uint64) {
	ln.blockOrderingMu.Lock()
	defer ln.blockOrderingMu.Unlock()

	ln.nextExpectedSlot = slot
	logx.Info("BLOCK:ORDERING", "Set next expected slot to", slot)
}

func (ln *Libp2pNetwork) ClearOrderingQueue() {
	ln.blockOrderingMu.Lock()
	defer ln.blockOrderingMu.Unlock()

	ln.blockOrderingQueue = make(map[uint64]*block.BroadcastedBlock)
}
