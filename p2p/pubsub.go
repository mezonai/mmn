package p2p

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
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

				if err := bs.MarkFinalized(vote.Slot); err != nil {
					return fmt.Errorf("mark block as finalized error: %w", err)
				}

				logx.Info("VOTE", "Block finalized via P2P! slot=", vote.Slot)
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

				if err := bs.AddBlockPending(blk); err != nil {
					continue
				}

				if err := ld.ApplyBlock(utils.BroadcastedBlockToBlock(blk)); err != nil {
					logx.Error("NETWORK:SYNC BLOCK", "Failed to apply block: ", err.Error())
					continue
				}

				if err := bs.MarkFinalized(blk.Slot); err != nil {
					logx.Error("NETWORK:SYNC BLOCK", "Failed to finalize synced block:", err)
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

	// Attach snapshot announce callback to trigger UDP download and set ready when completed
	ln.SetCallbacks(Callbacks{
		OnSnapshotAnnounce: func(ann SnapshotAnnounce) error {
			// Skip processing our own snapshot announcement
			if ann.PeerID == ln.selfPubKey || ann.UDPAddr == ln.getAnnounceUDPAddr() {
				logx.Info("SNAPSHOT:DOWNLOAD", "skip self announce", ann.UDPAddr)
				return nil
			}
			accountStore := ld.GetAccountStore()
			if accountStore == nil {
				return nil
			}
			provider := accountStore.GetDatabaseProvider()
			if provider == nil {
				return nil
			}
			down := snapshot.NewSnapshotDownloader(provider, "/data/snapshots")
			logx.Info("SNAPSHOT:DOWNLOAD", "start", ann.UDPAddr)
			go func() {
				task, err := down.DownloadSnapshotFromPeer(ln.ctx, ann.UDPAddr, ann.PeerID, ann.Slot, ann.ChunkSize)
				if err != nil {
					logx.Error("SNAPSHOT:DOWNLOAD", "start failed:", err)
					return
				}
				for {
					time.Sleep(2 * time.Second)
					st, ok := down.GetDownloadStatus(task.ID)
					if !ok || st == nil {
						continue
					}
					if st.Status == snapshot.TransferStatusComplete {
						logx.Info("SNAPSHOT:DOWNLOAD", "completed ", ann.UDPAddr)
						// Log readiness line without actually enabling, per request
						logx.Info("NETWORK:READY", "Node is ready after snapshot")
						break
					}
					if st.Status == snapshot.TransferStatusFailed || st.Status == snapshot.TransferStatusCancelled {
						logx.Error("SNAPSHOT:DOWNLOAD", "not successful:", st.Status)
						break
					}
				}
			}()
			return nil
		},
	})

	// Start UDP snapshot streamer for serving nodes
	accountStore := ld.GetAccountStore()
	if accountStore != nil {
		provider := accountStore.GetDatabaseProvider()
		if provider != nil {
			go func() {
				if err := snapshot.StartSnapshotUDPStreamer(provider, "/data/snapshots", ":9100"); err != nil {
					logx.Error("SNAPSHOT:STREAMER", "failed to start:", err)
				}
			}()
		}
	}

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

	if t, e := ln.pubsub.Join(TopicSnapshotAnnounce); e == nil {
		ln.topicSnapshotAnnounce = t
		if sub, e2 := ln.topicSnapshotAnnounce.Subscribe(); e2 == nil {
			exception.SafeGoWithPanic("HandleSnapshotAnnounce", func() {
				ln.handleSnapshotAnnounce(ctx, sub)
			})
			logx.Info("SNAPSHOT:GOSSIP", "joined topic", TopicSnapshotAnnounce)
		}
	}
	if t, e := ln.pubsub.Join(TopicSnapshotRequest); e == nil {
		ln.topicSnapshotRequest = t
		if sub, e2 := ln.topicSnapshotRequest.Subscribe(); e2 == nil {
			exception.SafeGoWithPanic("HandleSnapshotRequest", func() {
				ln.handleSnapshotRequest(ctx, sub)
			})
			logx.Info("SNAPSHOT:GOSSIP", "joined topic ", TopicSnapshotRequest)
		}
	}

	// start periodic snapshot announcer if topic available
	ln.startSnapshotAnnouncer()
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

// handleSnapshotAnnounce processes incoming snapshot announce messages and triggers HTTP download when needed
func (ln *Libp2pNetwork) handleSnapshotAnnounce(ctx context.Context, sub *pubsub.Subscription) {
	for {
		msg, err := sub.Next(ctx)
		if err != nil {
			return
		}
		var ann SnapshotAnnounce
		if err := json.Unmarshal(msg.Data, &ann); err != nil {
			continue
		}
		if ann.PeerID == ln.selfPubKey {
			continue
		}
		localSlot := ln.blockStore.GetLatestSlot()
		if ann.Slot > localSlot {
			logx.Info("SNAPSHOT:GOSSIP", "Announce received slot=", ann.Slot, " udp=", ann.UDPAddr)
			if ln.onSnapshotAnnounce != nil {
				_ = ln.onSnapshotAnnounce(ann)
			}
		}
	}
}

func (ln *Libp2pNetwork) handleSnapshotRequest(ctx context.Context, sub *pubsub.Subscription) {
	for {

		msg, err := sub.Next(ctx)
		if err != nil {
			return
		}

		if msg.ReceivedFrom.String() != ln.selfPubKey {
			continue
		}

		var req SnapshotRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			continue
		}

		// Check snapshot availability
		path := "/data/snapshots/snapshot-latest.json"
		fi, err := os.Stat(path)
		if err != nil {
			// no snapshot to announce
			logx.Info("SNAPSHOT:GOSSIP", "request received but no snapshot")
			continue
		}
		snap, err := snapshot.ReadSnapshot(path)
		if err != nil {
			logx.Error("SNAPSHOT:GOSSIP", "read snapshot error:", err)
			continue
		}

		ann := SnapshotAnnounce{
			Slot:      snap.Meta.Slot,
			BankHash:  fmt.Sprintf("%x", snap.Meta.BankHash[:]),
			Size:      fi.Size(),
			UDPAddr:   ln.getAnnounceUDPAddr(),
			ChunkSize: 16384,
			CreatedAt: time.Now().Unix(),
			PeerID:    ln.selfPubKey,
		}
		data, _ := json.Marshal(ann)
		if ln.topicSnapshotAnnounce != nil {
			_ = ln.topicSnapshotAnnounce.Publish(ctx, data)
			logx.Info("SNAPSHOT:GOSSIP", "announce in response slot=", ann.Slot)
			// simulate local delivery for single-node tests
			if ln.onSnapshotAnnounce != nil {
				_ = ln.onSnapshotAnnounce(ann)
			}
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
	if cbs.OnSnapshotAnnounce != nil {
		ln.onSnapshotAnnounce = cbs.OnSnapshotAnnounce
	}
}

// getAnnounceUDPAddr builds an ip:port string for the UDP snapshot streamer
func (ln *Libp2pNetwork) getAnnounceUDPAddr() string {
	// Optional override via environment variable
	if host := os.Getenv("SNAPSHOT_ANNOUNCE_HOST"); host != "" {
		addr := fmt.Sprintf("%s:%d", host, 9100)
		logx.Info("SNAPSHOT:GOSSIP", "announce UDP addr (env override)", addr)
		return addr
	}

	ip := ""
	// Track a non-loopback fallback if present
	for _, maddr := range ln.host.Addrs() {
		str := maddr.String()
		// naive extract /ip4/x.x.x.x
		if strings.HasPrefix(str, "/ip4/") {
			parts := strings.Split(str, "/")
			if len(parts) >= 3 {
				cand := parts[2]
				parsed := net.ParseIP(cand)
				if parsed != nil {
					if !parsed.IsUnspecified() && !parsed.IsLoopback() {
						ip = cand
						break
					}
				}
			}
		}
	}

	addr := fmt.Sprintf("%s:%d", ip, 9100)
	logx.Info("SNAPSHOT:GOSSIP", "announce UDP addr", addr)
	return addr
}

// startSnapshotAnnouncer periodically publishes SnapshotAnnounce if a latest snapshot exists
func (ln *Libp2pNetwork) startSnapshotAnnouncer() {
	if ln.topicSnapshotAnnounce == nil {
		logx.Info("SNAPSHOT:GOSSIP", "announce topic not ready; skip announcer")
		return
	}
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ln.ctx.Done():
				return
			case <-ticker.C:
				path := "/data/snapshots/snapshot-latest.json"
				fi, err := os.Stat(path)
				if err != nil {
					logx.Info("SNAPSHOT:GOSSIP", "no snapshot-latest.json")
					continue
				}
				snap, err := snapshot.ReadSnapshot(path)
				if err != nil {
					logx.Error("SNAPSHOT:GOSSIP", "read snapshot error:", err)
					continue
				}
				ann := SnapshotAnnounce{
					Slot:      snap.Meta.Slot,
					BankHash:  fmt.Sprintf("%x", snap.Meta.BankHash[:]),
					Size:      fi.Size(),
					UDPAddr:   ln.getAnnounceUDPAddr(),
					ChunkSize: 16384,
					CreatedAt: time.Now().Unix(),
					PeerID:    ln.selfPubKey,
				}
				data, _ := json.Marshal(ann)
				if err := ln.topicSnapshotAnnounce.Publish(ln.ctx, data); err == nil {
					logx.Info("SNAPSHOT:GOSSIP", "Announce published slot=", ann.Slot)
					// simulate local delivery to surface logs in single-node runs
					if ln.onSnapshotAnnounce != nil {
						_ = ln.onSnapshotAnnounce(ann)
					}
				}
			}
		}
	}()
}
