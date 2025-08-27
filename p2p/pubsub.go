package p2p

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/poh"
	"github.com/mezonai/mmn/snapshot"
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/utils"
)

func (ln *Libp2pNetwork) SetupCallbacks(ld *ledger.Ledger, privKey ed25519.PrivateKey, self config.NodeConfig, bs store.BlockStore, collector *consensus.Collector, mp *mempool.Mempool, recorder *poh.PohRecorder) {
	ln.SetCallbacks(Callbacks{
		OnBlockReceived: func(blk *block.BroadcastedBlock) error {
			if existingBlock := bs.Block(blk.Slot); existingBlock != nil {
				return nil
			}

			// Verify PoH
			logx.Info("BLOCK", "VerifyPoH: verifying PoH for block=", blk.Hash)
			if err := blk.VerifyPoH(); err != nil {
				logx.Error("BLOCK", "Invalid PoH:", err)
				return fmt.Errorf("invalid PoH")
			}

			// Verify block
			if err := ld.VerifyBlock(blk); err != nil {
				logx.Error("BLOCK", "Block verification failed: ", err)
				return err
			}

			if err := bs.AddBlockPending(blk); err != nil {
				logx.Error("BLOCK", "Failed to store block: ", err)
				return err
			}

			// Reset poh to sync poh clock with leader
			logx.Info("BLOCK", fmt.Sprintf("Resetting poh clock with leader at slot %d", blk.Slot))
			recorder.Reset(blk.LastEntryHash(), blk.Slot)

			vote := &consensus.Vote{
				Slot:      blk.Slot,
				BlockHash: blk.Hash,
				VoterID:   self.PubKey,
			}
			vote.Sign(privKey)

			committed, needApply, err := collector.AddVote(vote)
			if err != nil {
				logx.Error("VOTE", "Failed to add vote: ", err)
				return err
			}

			if committed && needApply {
				logx.Info("VOTE", "Block committed: slot=", vote.Slot)
				// Apply block to ledger
				block := bs.Block(vote.Slot)
				if block == nil {
					return fmt.Errorf("block not found for slot %d", vote.Slot)
				}
				if err := ld.ApplyBlock(block); err != nil {
					return fmt.Errorf("apply block error: %w", err)
				}

				// Mark block as finalized and publish transaction finalization events
				if err := bs.MarkFinalized(vote.Slot); err != nil {
					return fmt.Errorf("mark block as finalized error: %w", err)
				}

				logx.Info("VOTE", "Block finalized via P2P! slot=", vote.Slot)
			}

			ln.BroadcastVote(context.Background(), vote)

			return nil
		},
		OnVoteReceived: func(vote *consensus.Vote) error {
			logx.Info("VOTE", "Received vote from network: slot= ", vote.Slot, ",voter= ", vote.VoterID)
			committed, needApply, err := collector.AddVote(vote)
			if err != nil {
				logx.Error("VOTE", "Failed to add vote: ", err)
				return err
			}

			if committed && needApply {
				logx.Info("VOTE", "Block committed: slot=", vote.Slot)
				// Apply block to ledger
				block := bs.Block(vote.Slot)
				if block == nil {
					return fmt.Errorf("block not found for slot %d", vote.Slot)
				}
				if err := ld.ApplyBlock(block); err != nil {
					return fmt.Errorf("apply block error: %w", err)
				}

				// Mark block as finalized and publish transaction finalization events
				if err := bs.MarkFinalized(vote.Slot); err != nil {
					return fmt.Errorf("mark block as finalized error: %w", err)
				}

				logx.Info("VOTE", "Block finalized via P2P! slot=", vote.Slot)

				writeSnapshotIfDue(ld, vote.Slot)
			}

			return nil
		},
		OnTransactionReceived: func(txData *transaction.Transaction) error {
			logx.Info("TX", "Processing received transaction from P2P network")

			// Add transaction to mempool
			_, err := mp.AddTx(txData, false)
			if err != nil {
				fmt.Printf("Failed to add transaction from P2P: %v\n", err)
			}
			return nil
		},
		OnSyncResponseReceived: func(blocks []*block.BroadcastedBlock) error {
			logx.Info("NETWORK:SYNC BLOCK", "Processing ", len(blocks), " blocks from sync response")
			for _, blk := range blocks {
				if blk == nil {
					continue
				}

				// skip add pending if block already exists
				if existingBlock := bs.Block(blk.Slot); existingBlock != nil {
					continue
				}

				// Verify PoH
				if err := blk.VerifyPoH(); err != nil {
					logx.Error("NETWORK:SYNC BLOCK", "Invalid PoH for synced block: ", err)
					continue
				}

				// Verify block
				if err := ld.VerifyBlock(blk); err != nil {
					logx.Error("NETWORK:SYNC BLOCK", "Block verification failed for synced block: ", err)
					continue
				}

				if err := ld.ApplyBlock(utils.BroadcastedBlockToBlock(blk)); err != nil {
					logx.Error("NETWORK:SYNC BLOCK", "Failed to apply block: ", err.Error())
					continue
				}

				logx.Info("NETWORK:SYNC BLOCK", fmt.Sprintf("Successfully processed synced block: slot=%d", blk.Slot))
			}

			// Do not request latest slot here to avoid loops; handled after stream end
			return nil
		},
		OnLatestSlotReceived: func(latestSlot uint64, peerID string) error {
			localLatestSlot := bs.GetLatestSlot()
			if latestSlot > localLatestSlot {
				fromSlot := localLatestSlot + 1
				logx.Info("NETWORK:SYNC BLOCK", "Peer has higher slot:", latestSlot, "local slot:", localLatestSlot, "requesting sync from slot:", fromSlot)

				ctx := context.Background()
				// Only request block sync if we are beyond ready threshold to avoid churn
				if latestSlot-localLatestSlot > ReadyGapThreshold {
					if err := ln.RequestBlockSync(ctx, fromSlot); err != nil {
						logx.Error("NETWORK:SYNC BLOCK", "Failed to send sync request after latest slot:", err)
					}
				}
			}

			// mark ready when gap small enough, and enable full handlers once
			gap := uint64(0)
			if latestSlot > localLatestSlot {
				gap = latestSlot - localLatestSlot
			}
			if gap <= ReadyGapThreshold && !ln.IsNodeReady() {
				ln.enableFullModeOnce.Do(func() {
					if err := ln.setupHandlers(ln.ctx, nil); err != nil {
						logx.Error("NETWORK:READY", "Failed to setup handlers:", err.Error())
						return
					}
					ln.setNodeReady()
					logx.Info("NETWORK:READY", "Node is ready. Full handlers enabled.")
				})
			}
			return nil
		},
	})

	go ln.startInitialSync(bs)

	go ln.startPeriodicSyncCheck(bs)

	go ln.startCleanupRoutine()
}

func (ln *Libp2pNetwork) setupSyncNodeTopics(ctx context.Context) {
	var err error

	if ln.topicBlockSyncReq, err = ln.pubsub.Join(BlockSyncRequestTopic); err == nil {
		if sub, err := ln.topicBlockSyncReq.Subscribe(); err == nil {
			exception.SafeGoWithPanic("handleBlockSyncRequestTopic", func() {
				ln.handleBlockSyncRequestTopic(ctx, sub)
			})

		}
	}
	if ln.topicLatestSlot, err = ln.pubsub.Join(LatestSlotTopic); err == nil {
		if sub, err := ln.topicLatestSlot.Subscribe(); err == nil {
			exception.SafeGoWithPanic("handleBlockSyncResponseTopic", func() {
				ln.HandleLatestSlotTopic(ctx, sub)
			})
		}
	}
}

func (ln *Libp2pNetwork) SetupPubSubTopics(ctx context.Context) {
	var err error

	if ln.topicBlocks, err = ln.pubsub.Join(TopicBlocks); err == nil {
		if sub, err := ln.topicBlocks.Subscribe(); err == nil {
			exception.SafeGoWithPanic("HandleBlockTopic", func() {
				ln.HandleBlockTopic(ctx, sub)
			})
		}
	}

	if ln.topicVotes, err = ln.pubsub.Join(TopicVotes); err == nil {
		if sub, err := ln.topicVotes.Subscribe(); err == nil {
			exception.SafeGoWithPanic("HandleVoteTopic", func() {
				ln.HandleVoteTopic(ctx, sub)
			})
		}
	}

	if ln.topicTxs, err = ln.pubsub.Join(TopicTxs); err == nil {
		if sub, err := ln.topicTxs.Subscribe(); err == nil {
			exception.SafeGoWithPanic("HandleTransactionTopic", func() {
				ln.HandleTransactionTopic(ctx, sub)
			})
		}
	}

}

func (ln *Libp2pNetwork) SetCallbacks(cbs Callbacks) {
	if cbs.OnBlockReceived != nil {
		ln.onBlockReceived = cbs.OnBlockReceived
	}
	if cbs.OnVoteReceived != nil {
		ln.onVoteReceived = cbs.OnVoteReceived
	}
	if cbs.OnTransactionReceived != nil {
		ln.onTransactionReceived = cbs.OnTransactionReceived
	}
	if cbs.OnLatestSlotReceived != nil {
		ln.onLatestSlotReceived = cbs.OnLatestSlotReceived
	}
	if cbs.OnSyncResponseReceived != nil {
		ln.onSyncResponseReceived = cbs.OnSyncResponseReceived
	}
}

func writeSnapshotIfDue(ld *ledger.Ledger, slot uint64) {
	if slot%RangeForSnapshot != 0 { // adjust interval as needed
		return
	}
	accountStore := ld.GetAccountStore()
	if accountStore == nil {
		return
	}
	dbProvider := accountStore.GetDatabaseProvider()
	if dbProvider == nil {
		return
	}
	bankHash, err := snapshot.ComputeFullBankHash(dbProvider)
	if err != nil {
		logx.Error("SNAPSHOT", fmt.Sprintf("BankHash compute failed at slot %d: %v", slot, err))
		return
	}
	dir := "/data/snapshots"
	// Write a new snapshot for this slot to a slot-specific file
	saved, err := snapshot.WriteSnapshotWithDefaults(dir, dbProvider, slot, bankHash, nil)
	if err != nil {
		logx.Error("SNAPSHOT", fmt.Sprintf("Failed to write snapshot at slot %d: %v", slot, err))
		return
	}

	// Atomically update latest snapshot pointer
	latest := filepath.Join(dir, "snapshot-latest.json")
	tmpLatest := latest + ".tmp"
	if err := os.Rename(saved, tmpLatest); err != nil {
		logx.Error("SNAPSHOT", fmt.Sprintf("Failed to move snapshot to temp latest: %v", err))
		return
	}
	if err := os.Rename(tmpLatest, latest); err != nil {
		logx.Error("SNAPSHOT", fmt.Sprintf("Failed to finalize latest snapshot: %v", err))
		return
	}

	logx.Info("SNAPSHOT", fmt.Sprintf("Updated latest snapshot: %s (slot %d)", latest, slot))
}
