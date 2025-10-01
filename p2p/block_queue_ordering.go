package p2p

import (
	"fmt"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/poh"
	"github.com/mezonai/mmn/utils"
)

func (ln *Libp2pNetwork) AddBlockToQueueOrdering(blk *block.BroadcastedBlock, ledger *ledger.Ledger, collector *consensus.Collector, latestSlot uint64) error {
	if blk == nil {
		return fmt.Errorf("block is nil")
	}

	ln.blockQueueOrderingMu.Lock()
	defer ln.blockQueueOrderingMu.Unlock()

	if _, exists := ln.blockQueueOrdering[blk.Slot]; exists {
		logx.Debug("BLOCK:QUEUE:ORDERING", "Block already exists for slot", blk.Slot)
		return nil
	}

	ln.blockQueueOrdering[blk.Slot] = blk
	logx.Info("BLOCK:QUEUE:ORDERING", "Added block to queue ordering with slot", blk.Slot)

	return ln.processConsecutiveBlocksInQueue(ledger, collector, latestSlot)
}

func (ln *Libp2pNetwork) processConsecutiveBlocksInQueue(ledger *ledger.Ledger, collector *consensus.Collector, latestSlot uint64) error {
	var processedBlocks []*block.BroadcastedBlock

	nextSlot := ln.getNextExpectedSlotForQueue()

	for {
		if blk, exists := ln.blockQueueOrdering[nextSlot]; exists {
			processedBlocks = append(processedBlocks, blk)
			delete(ln.blockQueueOrdering, nextSlot)
			ln.nextExpectedSlotForQueue = nextSlot + 1
			nextSlot++
		} else if ln.isLeaderOfSlot(nextSlot) {
			prevHash := ln.getPrevHashForSlot(nextSlot, ln.blockStore, processedBlocks)
			entries := poh.GenerateTickOnlyEntries(prevHash, int(ln.pohCfg.TicksPerSlot), ln.pohCfg.HashesPerTick)
			emptyBlock := block.AssembleBlock(
				nextSlot,
				prevHash,
				ln.selfPubKey,
				entries,
			)
			emptyBlock.Sign(ln.selfPrivKey)

			processedBlocks = append(processedBlocks, emptyBlock)
			ln.nextExpectedSlotForQueue = nextSlot + 1
			nextSlot++
			continue
		} else {
			leaderPeerID, leaderExists := ln.GetLeaderForSlot(nextSlot)
			if !leaderExists || !ln.IsPeerConnected(leaderPeerID) {
				ln.nextExpectedSlotForQueue = nextSlot + 1
				nextSlot++
				logx.Debug("BLOCK:QUEUE:ORDERING", "is not connected, breaking block processing", nextSlot)
				break
			}

			if ln.topicMissingBlockReq != nil {
				ln.retryMissingBlockAsync(nextSlot, MaxMissingRetry, MissingRetryInterval)
			}
			break
		}
	}

	for _, blk := range processedBlocks {
		if err := ln.processBlockInQueue(ledger, blk, collector, latestSlot); err != nil {
			logx.Warn("BLOCK:QUEUE:ORDERING", "Failed to process block at slot", blk.Slot, ":", err.Error())
			continue
		}
	}

	if len(processedBlocks) > 0 {
		logx.Info("BLOCK:QUEUE:ORDERING", "Processed", len(processedBlocks), "consecutive blocks")
	}

	return nil
}

func (ln *Libp2pNetwork) processBlockInQueue(ld *ledger.Ledger, blk *block.BroadcastedBlock, collector *consensus.Collector, latestSlot uint64) error {
	committed, needApply := collector.CheckVoteStatus(blk.Slot)
	logx.Warn("Check Vote Status", committed, needApply)
	if committed && needApply {
		if err := ld.ApplyBlock(utils.BroadcastedBlockToBlock(blk)); err != nil {
			return fmt.Errorf("apply block error: %w", err)
		}

		logx.Info("VOTE", "Block finalized via P2P! slot=", blk.Slot)
	}

	if blk.Slot >= latestSlot {
		ln.checkForMissingBlocksAround(ln.blockStore, blk.Slot, false)
	} else {
		ln.checkForMissingBlocksAround(ln.blockStore, blk.Slot, true)
	}
	return nil
}

func (ln *Libp2pNetwork) getNextExpectedSlotForQueue() uint64 {
	return ln.nextExpectedSlotForQueue
}

func (ln *Libp2pNetwork) hasBlockInOrderingQueue(slot uint64) bool {
	ln.blockQueueOrderingMu.RLock()
	defer ln.blockQueueOrderingMu.RUnlock()
	_, exists := ln.blockQueueOrdering[slot]
	return exists
}

func (ln *Libp2pNetwork) SetNextExpectedSlotForQueue(slot uint64) {
	ln.blockQueueOrderingMu.Lock()
	defer ln.blockQueueOrderingMu.Unlock()

	ln.nextExpectedSlotForQueue = slot
	logx.Info("BLOCK:QUEUE:ORDERING", "Set next expected slot to", slot)
}

func (ln *Libp2pNetwork) ProcessBlockBeforeBroadcast(blk *block.BroadcastedBlock, ledger *ledger.Ledger, mempool *mempool.Mempool, collector *consensus.Collector, latestSlot uint64) error {

	if err := ln.blockStore.AddBlockPending(blk); err != nil {
		logx.Error("BLOCK:PROCESS:BEFORE:BROADCAST", "Failed to store block:", err)
		return err
	}

	vote := &consensus.Vote{Slot: blk.Slot, BlockHash: blk.Hash, VoterID: ln.selfPubKey}
	vote.Sign(ln.selfPrivKey)

	if err := ln.ProcessVote(ln.blockStore, ledger, mempool, vote, collector); err != nil {
		return err
	}
	ln.BroadcastVote(ln.ctx, vote)
	err := ln.AddBlockToQueueOrdering(blk, ledger, collector, latestSlot)
	return err
}
