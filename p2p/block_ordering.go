package p2p

import (
	"context"
	"fmt"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/poh"
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/utils"
)

func (ln *Libp2pNetwork) AddBlockToOrderingQueue(blk *block.BroadcastedBlock, bs store.BlockStore, ld *ledger.Ledger) (*block.BroadcastedBlock, error) {
	if blk == nil {
		return nil, fmt.Errorf("block is nil")
	}

	ln.blockOrderingMu.Lock()
	defer ln.blockOrderingMu.Unlock()

	// Skip if block already exists in store
	if existingBlock := bs.HasCompleteBlock(blk.Slot); existingBlock {
		return nil, nil
	}

	// Add block to queue
	ln.blockOrderingQueue[blk.Slot] = blk

	return ln.processConsecutiveBlocks(bs, ld)
}

func (ln *Libp2pNetwork) processConsecutiveBlocks(bs store.BlockStore, ld *ledger.Ledger) (*block.BroadcastedBlock, error) {
	var processedBlocks []*block.BroadcastedBlock
	var emptyBlocksToBroadcast []*block.BroadcastedBlock
	// Find consecutive blocks starting from nextExpectedSlot
	for {
		if ln.nextExpectedSlot > ln.worldLatestSlot {
			break
		} else if blk, exists := ln.blockOrderingQueue[ln.nextExpectedSlot]; exists {
			processedBlocks = append(processedBlocks, blk)
			delete(ln.blockOrderingQueue, ln.nextExpectedSlot)
			ln.nextExpectedSlot++
		} else if ln.isLeaderOfSlot(ln.nextExpectedSlot) {
			// Create empty block for leader slot
			prevHash := ln.getPrevHashForSlot(ln.nextExpectedSlot, bs, processedBlocks)
			// Use PoH config instead of hardcoded values
			entries := poh.GenerateTickOnlyEntries(prevHash, int(ln.pohCfg.TicksPerSlot), ln.pohCfg.HashesPerTick)
			emptyBlock := block.AssembleBlock(
				ln.nextExpectedSlot,
				prevHash,
				ln.selfPubKey,
				entries,
			)

			emptyBlock.Sign(ln.selfPrivKey)

			// Add to processed blocks
			processedBlocks = append(processedBlocks, emptyBlock)
			// Add to empty blocks array for later broadcast
			emptyBlocksToBroadcast = append(emptyBlocksToBroadcast, emptyBlock)

			ln.nextExpectedSlot++
			continue
		} else {
			break
		}
	}

	// Process all consecutive blocks
	for _, blk := range processedBlocks {
		if err := ln.processBlock(blk, bs, ld); err != nil {
			logx.Warn("BLOCK:ORDERING", "Failed to process block at slot", err.Error())
			continue
		}
	}

	if len(processedBlocks) > 0 {
		logx.Info("BLOCK:ORDERING", "Processed", len(processedBlocks), "consecutive blocks, next expected slot:", ln.nextExpectedSlot)
	}

	// Broadcast empty blocks if any were created
	if len(emptyBlocksToBroadcast) > 0 {
		if err := ln.BroadcastEmptyBlocks(context.Background(), emptyBlocksToBroadcast); err != nil {
			logx.Error("BLOCK:ORDERING", "Failed to broadcast empty blocks:", err)
		} else {
			logx.Info("BLOCK:ORDERING", "Successfully broadcasted", len(emptyBlocksToBroadcast), "empty blocks")
		}
	}

	if len(processedBlocks) == 0 {
		return nil, nil
	}
	return processedBlocks[len(processedBlocks)-1], nil
}

func (ln *Libp2pNetwork) processBlock(blk *block.BroadcastedBlock, bs store.BlockStore, ld *ledger.Ledger) error {
	// Verify PoH
	if err := blk.VerifyPoH(); err != nil {
		return fmt.Errorf("invalid PoH for block at slot %d: %w", blk.Slot, err)
	}

	if err := bs.AddBlockPending(blk); err != nil {
		return fmt.Errorf("add pending block error: %w", err)
	}

	if err := ld.ApplyBlock(utils.BroadcastedBlockToBlock(blk)); err != nil {
		return fmt.Errorf("apply block error: %w", err)
	}

	if err := bs.MarkFinalized(blk.Slot); err != nil {
		return fmt.Errorf("failed to finalize block at slot %d: %w", blk.Slot, err)
	}

	// Remove from missing blocks tracker
	ln.removeFromMissingTracker(blk.Slot)

	logx.Info("BLOCK:ORDERING", "Successfully processed block at slot", blk.Slot)
	return nil
}

func (ln *Libp2pNetwork) SetNextExpectedSlot(slot uint64) {
	ln.blockOrderingMu.Lock()
	defer ln.blockOrderingMu.Unlock()

	ln.nextExpectedSlot = slot
	logx.Info("BLOCK:ORDERING", "Set next expected slot to", slot)
}

func (ln *Libp2pNetwork) isLeaderOfSlot(slot uint64) bool {
	if leader, ok := ln.leaderSchedule.LeaderAt(slot); ok && leader == ln.selfPubKey {
		return true
	}
	return false
}

func (ln *Libp2pNetwork) getPrevHashForSlot(slot uint64, bs store.BlockStore, processedBlocks []*block.BroadcastedBlock) [32]byte {
	prevSlot := slot - 1
	if prevSlot == 0 {
		return [32]byte{}
	}

	// Check in processed blocks (blocks being processed in current batch)
	if len(processedBlocks) > 0 {
		lastProcessedBlock := processedBlocks[len(processedBlocks)-1]
		if lastProcessedBlock.Slot == prevSlot {
			return lastProcessedBlock.LastEntryHash()
		}
	} else {
		// First check in block store
		prevBlock := bs.Block(prevSlot)
		if prevBlock != nil {
			return prevBlock.LastEntryHash()
		}
	}

	logx.Warn("BLOCK:ORDERING", "No previous hash found for slot", prevSlot)
	return [32]byte{}
}
