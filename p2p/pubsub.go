package p2p

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
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

			if err := bs.AddBlockPending(blk); err != nil {
				logx.Error("BLOCK", "Failed to store block: ", err)
				return err
			}

			if len(ln.host.Network().Peers()) > 0 {
				go ln.checkForMissingBlocksAround(bs, blk.Slot)
			}
			// Reset poh to sync poh clock with leader
			if blk.Slot > bs.GetLatestSlot() {
				logx.Info("BLOCK", fmt.Sprintf("Resetting poh clock with leader at slot %d", blk.Slot))
				if err := ln.OnSyncPohFromLeader(blk.LastEntryHash(), blk.Slot); err != nil {
					logx.Error("BLOCK", "Failed to sync poh from leader: ", err)
				}
			}

			vote := &consensus.Vote{
				Slot:      blk.Slot,
				BlockHash: blk.Hash,
				VoterID:   self.PubKey,
			}
			vote.Sign(privKey)

			// verify passed broadcast vote
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

			// not leader => maybe vote come before block received => if dont have block just return
			if bs.Block(vote.Slot) == nil {
				logx.Info("VOTE", "Received vote from network: slot= ", vote.Slot, ",voter= ", vote.VoterID, " but dont have block")
				return nil
			}
			if committed && needApply {
				logx.Info("VOTE", "Committed vote from OnVoteReceived: slot= ", vote.Slot, ",voter= ", vote.VoterID)
				err := ln.applyDataToBlock(vote, bs, ld, mp)
				if err != nil {
					logx.Error("VOTE", "Failed to apply data to block: ", err)
					return err
				}
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
				// Add to block store and publish transaction inclusion events
				if err := bs.AddBlockPending(blk); err != nil {
					logx.Error("NETWORK:SYNC BLOCK", "Failed to store synced block: ", err)
					continue
				}

				// Remove from missing blocks tracker if it was there
				ln.removeFromMissingTracker(blk.Slot)

				logx.Info("NETWORK:SYNC BLOCK", fmt.Sprintf("Successfully processed synced block: slot=%d", blk.Slot))
			}

			return nil
		},
		OnLatestSlotReceived: func(latestSlot uint64, peerID string) error {
			// localLatestSlot := bs.GetLatestSlot()
			// if latestSlot > localLatestSlot {
			// 	fromSlot := localLatestSlot + 1

			// 	ctx := context.Background()
			// 	// Only request block sync if we are beyond ready threshold to avoid churn
			// 	if latestSlot-localLatestSlot > ReadyGapThreshold {
			// 		if err := ln.RequestBlockSync(ctx, fromSlot); err != nil {
			// 			logx.Error("NETWORK:SYNC BLOCK", "Failed to send sync request after latest slot:", err)
			// 		}
			// 	}
			// }

			// // mark ready when gap small enough, and enable full handlers once
			// gap := uint64(0)
			// if latestSlot > localLatestSlot {
			// 	gap = latestSlot - localLatestSlot
			// }
			// if gap <= ReadyGapThreshold && !ln.IsNodeReady() {
			// 	ln.enableFullModeOnce.Do(func() {
			// 		if err := ln.setupHandlers(ln.ctx, nil); err != nil {
			// 			logx.Error("NETWORK:READY", "Failed to setup handlers:", err.Error())
			// 			return
			// 		}
			// 		ln.setNodeReady()
			// 		logx.Info("NETWORK:READY", "Node is ready. Full handlers enabled.")
			// 	})
			// }
			return nil
		},
	})

	// Attach snapshot announce callback to trigger UDP download and set ready when completed
	ln.SetCallbacks(Callbacks{
		OnSnapshotAnnounce: func(ann SnapshotAnnounce) error {
			logx.Info("SNAPSHOT:DEBUG", "Received snapshot announce from:", ann.PeerID, "UDP:", ann.UDPAddr, "Slot:", ann.Slot)
			logx.Info("SNAPSHOT:DEBUG", "UDP validation - addr:", ann.UDPAddr, "empty:", ann.UDPAddr == "", "hasPrefix:", strings.HasPrefix(ann.UDPAddr, ":"))

			// Skip self announces
			if ann.PeerID == ln.selfPubKey {
				logx.Info("SNAPSHOT:DOWNLOAD", "skip self announce", ann.UDPAddr)
				return nil
			}
			// Ensure UDP addr non-empty and valid
			if ann.UDPAddr == "" || strings.HasPrefix(ann.UDPAddr, ":") {
				logx.Error("SNAPSHOT:DOWNLOAD", "invalid announce UDP addr, skip", ann.UDPAddr)
				return nil
			}
			// Skip if local is already at or ahead of announced slot
			localSlot := ln.blockStore.GetLatestSlot()
			if ann.Slot <= localSlot {
				logx.Info("SNAPSHOT:DOWNLOAD", "skip announce; local at/above slot", "local=", localSlot, "ann=", ann.Slot)
				return nil
			}
			// Check if we already have a snapshot >= announced slot
			path := snapshot.GetSnapshotPath()
			if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
				if snap, err := snapshot.ReadSnapshot(path); err == nil && snap != nil {
					if snap.Meta.Slot >= ann.Slot {
						logx.Info("SNAPSHOT:DOWNLOAD", "skip announce; local snapshot up-to-date", "snap=", snap.Meta.Slot, "ann=", ann.Slot)
						return nil
					}
				}
			}
			// If no local snapshot or local snapshot is older, proceed with download
			logx.Info("SNAPSHOT:DOWNLOAD", "proceeding with download", "announced_slot=", ann.Slot, "local_path=", path)
			accountStore := ld.GetAccountStore()
			if accountStore == nil {
				return nil
			}
			provider := accountStore.GetDatabaseProvider()
			if provider == nil {
				return nil
			}
			// Use single snapshot directory
			down := snapshot.NewSnapshotDownloader(provider, snapshot.SnapshotDirectory)
			logx.Info("SNAPSHOT:DOWNLOAD", "start", ann.UDPAddr)
			go func() {
				logx.Info("SNAPSHOT:DEBUG", "Starting download from peer:", ann.PeerID, "UDP:", ann.UDPAddr, "Slot:", ann.Slot)
				task, err := down.DownloadSnapshotFromPeer(ln.ctx, ann.UDPAddr, ann.PeerID, ann.Slot, ann.ChunkSize)
				if err != nil {
					logx.Error("SNAPSHOT:DOWNLOAD", "start failed:", err)
					return
				}
				logx.Info("SNAPSHOT:DEBUG", "Download task created:", task.ID)
				for {
					time.Sleep(2 * time.Second)
					st, ok := down.GetDownloadStatus(task.ID)
					if !ok || st == nil {
						continue
					}
					if st.Status == snapshot.TransferStatusComplete {
						logx.Info("SNAPSHOT:DOWNLOAD", "completed ", ann.UDPAddr)
						// After snapshot load, apply leader schedule (if present), reset recorder, mark ready, and request block sync
						ln.enableFullModeOnce.Do(func() {
							if err := ln.setupHandlers(ln.ctx, nil); err != nil {
								logx.Error("NETWORK:READY", "Failed to setup handlers:", err.Error())
								return
							}
							// Apply leader schedule from snapshot if available
							// Apply leader schedule from snapshot if available
							// Try to find snapshot in multiple directories
							snapshotDirs := []string{"./snapshots", "./data/snapshots", "/data/snapshots", "./node-data/snapshots"}
							for _, dir := range snapshotDirs {
								path := filepath.Join(dir, "snapshot-latest.json")
								if snap, err := snapshot.ReadSnapshot(path); err == nil && snap != nil {
									if len(snap.Meta.LeaderSchedule) > 0 {
										if ls, err := poh.NewLeaderSchedule(snap.Meta.LeaderSchedule); err == nil {
											if ln.applyLeaderSchedule != nil {
												ln.applyLeaderSchedule(ls)
												logx.Info("SNAPSHOT:DOWNLOAD", "Applied leader schedule from snapshot")
											}
											if recorder != nil {
												recorder.SetLeaderSchedule(ls)
											}
										}
									}
									break
								}
							}
							// reset recorder to the snapshot slot boundary
							if recorder != nil {
								var zero [32]byte
								recorder.Reset(zero, ann.Slot)
							}
							logx.Info("SNAPSHOT:DOWNLOAD", "Snapshot loaded, starting block sync")
						})
						// Trigger block sync from the announced slot + 1 to latest slot
						go func() {
							// Simple delay to ensure topics are initialized
							time.Sleep(1 * time.Second)

							ctx := context.Background()
							// First sync from snapshot slot + 1
							if err := ln.RequestBlockSync(ctx, ann.Slot+1); err != nil {
								logx.Error("NETWORK:SYNC BLOCK", "Failed to request block sync after snapshot:", err)
								return
							}

							logx.Info("NETWORK:SYNC BLOCK", "Block sync requested from slot", ann.Slot+1)

							// Then request latest slot from peers to sync to the very latest
							latestSlot, err := ln.RequestLatestSlotFromPeers(ctx)
							if err != nil {
								logx.Warn("NETWORK:SYNC BLOCK", "Failed to get latest slot from peers:", err)
							} else {
								// Request sync from current local slot to latest network slot
								localSlot := ln.blockStore.GetLatestSlot()
								if latestSlot > localSlot {
									logx.Info("NETWORK:SYNC BLOCK", "Requesting sync to latest slot:", latestSlot, "from local slot:", localSlot)
									if err := ln.RequestBlockSync(ctx, localSlot+1); err != nil {
										logx.Error("NETWORK:SYNC BLOCK", "Failed to request sync to latest slot:", err)
									}
								}
							}

							// Mark node ready after snapshot download and block sync
							ln.enableFullModeOnce.Do(func() {
								if err := ln.setupHandlers(ln.ctx, nil); err != nil {
									logx.Error("NETWORK:READY", "Failed to setup handlers:", err.Error())
									return
								}
								ln.setNodeReady()
								logx.Info("NETWORK:READY", "Node is ready after snapshot download and block sync")
							})
						}()
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

	go ln.startInitialSync(bs)
	// Start UDP snapshot streamer for serving nodes
	accountStore := ld.GetAccountStore()
	if accountStore != nil {
		provider := accountStore.GetDatabaseProvider()
		if provider != nil {
			go func() {
				// Use single snapshot directory
				if err := snapshot.StartSnapshotUDPStreamer(provider, snapshot.SnapshotDirectory, ":9100"); err != nil {
					logx.Error("SNAPSHOT:STREAMER", "failed to start:", err)
				}
			}()
		}
	}

	go ln.startPeriodicSyncCheck(bs)

	// Start continuous gap detection
	go ln.startContinuousGapDetection(bs)

	// clean sync request expireds every 1 minute
	go ln.startCleanupRoutine()
}

func (ln *Libp2pNetwork) applyDataToBlock(vote *consensus.Vote, bs store.BlockStore, ld *ledger.Ledger, mp *mempool.Mempool) error {
	// Lock to ensure thread safety for concurrent apply processing
	ln.applyBlockMu.Lock()
	defer ln.applyBlockMu.Unlock()

	logx.Info("VOTE", "Block committed: slot=", vote.Slot)
	// check block apply or not if apply log and return
	if bs.IsApplied(vote.Slot) {
		logx.Info("VOTE", "Block already applied: slot=", vote.Slot)
		return nil
	}
	// Apply block to ledger
	block := bs.Block(vote.Slot)
	if block == nil {
		// missing block how to handle
		return fmt.Errorf("block not found for slot %d", vote.Slot)
	}
	if err := ld.ApplyBlock(block); err != nil {
		return fmt.Errorf("apply block error: %w", err)
	}

	// Mark block as finalized
	if err := bs.MarkFinalized(vote.Slot); err != nil {
		return fmt.Errorf("mark block as finalized error: %w", err)
	}

	logx.Info("VOTE", "Block finalized via P2P! slot=", vote.Slot)
	go mp.BlockCleanup(block)
	return nil
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

	if ln.topicBlockSyncReq == nil {
		if topic, err := ln.pubsub.Join(BlockSyncRequestTopic); err == nil {
			ln.topicBlockSyncReq = topic
			if sub, err := ln.topicBlockSyncReq.Subscribe(); err == nil {
				exception.SafeGoWithPanic("handleBlockSyncRequestTopic", func() {
					ln.handleBlockSyncRequestTopic(ctx, sub)
				})
				logx.Info("NETWORK:TOPIC", "joined topic", BlockSyncRequestTopic)
			} else {
				logx.Error("NETWORK:TOPIC", "failed to subscribe to", BlockSyncRequestTopic, "error:", err)
			}
		} else {
			logx.Error("NETWORK:TOPIC", "failed to join topic", BlockSyncRequestTopic, "error:", err)
		}
	}

	if ln.topicLatestSlot, err = ln.pubsub.Join(LatestSlotTopic); err == nil {
		if sub, err := ln.topicLatestSlot.Subscribe(); err == nil {
			exception.SafeGoWithPanic("handleBlockSyncResponseTopic", func() {
				ln.HandleLatestSlotTopic(ctx, sub)
			})
		}
	}

	if ln.topicCheckpointRequest, err = ln.pubsub.Join(CheckpointRequestTopic); err == nil {
		if sub, err := ln.topicCheckpointRequest.Subscribe(); err == nil {
			exception.SafeGoWithPanic("HandleCheckpointRequestTopic", func() {
				ln.HandleCheckpointRequestTopic(ctx, sub)
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
	ln.startSnapshotAnnouncer()

	// Request snapshot when joining network
	go func() {
		// Wait a bit for topics to be ready
		time.Sleep(5 * time.Second)

		// Check if network has any snapshots
		hasSnapshot := ln.checkNetworkHasSnapshot()
		if !hasSnapshot {
			// No snapshots in network, this is the first node - start immediately
			logx.Info("NETWORK:READY", "No snapshots in network, starting as first node")
			ln.enableFullModeOnce.Do(func() {
				if err := ln.setupHandlers(ln.ctx, nil); err != nil {
					logx.Error("NETWORK:READY", "Failed to setup handlers:", err.Error())
					return
				}
				ln.setNodeReady()
				logx.Info("NETWORK:READY", "Node is ready as first node")
			})
		} else {
			// Network has snapshots, check join behavior
			if ln.joinAfterSync {
				// Option 2: Wait for snapshot download and block sync before joining
				logx.Info("NETWORK:JOIN", "Join after sync enabled, waiting for snapshot download and block sync")
				ln.requestSnapshotOnJoin()
			} else {
				// Option 1: Join network immediately
				logx.Info("NETWORK:JOIN", "Join immediately enabled, starting network handlers")
				ln.enableFullModeOnce.Do(func() {
					if err := ln.setupHandlers(ln.ctx, nil); err != nil {
						logx.Error("NETWORK:READY", "Failed to setup handlers:", err.Error())
						return
					}
					ln.setNodeReady()
					logx.Info("NETWORK:READY", "Node is ready (joined immediately)")
				})
				// Still request snapshot in background for data consistency
				go ln.requestSnapshotOnJoin()
			}
		}
	}()

}

// HandleCheckpointRequestTopic listens for checkpoint hash requests and responds with local hash
func (ln *Libp2pNetwork) HandleCheckpointRequestTopic(ctx context.Context, sub *pubsub.Subscription) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}
			if msg.ReceivedFrom == ln.host.ID() {
				continue
			}

			var req CheckpointHashRequest
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				logx.Error("NETWORK:CHECKPOINT", "Failed to unmarshal checkpoint request:", err)
				continue
			}

			logx.Info("NETWORK:CHECKPOINT", "Received checkpoint request from", msg.ReceivedFrom.String(), "checkpoint=", req.Checkpoint)

			localSlot, localHash, ok := ln.getCheckpointHash(req.Checkpoint)
			if !ok {
				logx.Warn("NETWORK:CHECKPOINT", "No local block for checkpoint", req.Checkpoint)
				continue
			}

			logx.Info("NETWORK:CHECKPOINT", "Sending checkpoint response to", msg.ReceivedFrom.String(), "checkpoint=", req.Checkpoint)
			ln.sendCheckpointResponse(msg.ReceivedFrom, CheckpointHashResponse{
				Checkpoint: req.Checkpoint,
				Slot:       localSlot,
				BlockHash:  localHash,
				PeerID:     ln.host.ID().String(),
			})
		}
	}
}

func (ln *Libp2pNetwork) getCheckpointHash(checkpoint uint64) (uint64, [32]byte, bool) {
	if checkpoint == 0 {
		return 0, [32]byte{}, false
	}
	latest := ln.blockStore.GetLatestSlot()
	if latest == 0 {
		return 0, [32]byte{}, false
	}
	if latest < checkpoint {
		return 0, [32]byte{}, false
	}
	blk := ln.blockStore.Block(checkpoint)
	if blk == nil {
		return 0, [32]byte{}, false
	}
	return checkpoint, blk.Hash, true
}

func (ln *Libp2pNetwork) sendCheckpointResponse(targetPeer peer.ID, resp CheckpointHashResponse) {
	stream, err := ln.host.NewStream(context.Background(), targetPeer, CheckpointProtocol)
	if err != nil {
		logx.Error("NETWORK:CHECKPOINT", "Failed to open checkpoint stream:", err)
		return
	}
	defer stream.Close()

	data, err := json.Marshal(resp)
	if err != nil {
		logx.Error("NETWORK:CHECKPOINT", "Failed to marshal checkpoint response:", err)
		return
	}
	if _, err := stream.Write(data); err != nil {
		logx.Error("NETWORK:CHECKPOINT", "Failed to write checkpoint response:", err)
		return
	}
	logx.Info("NETWORK:CHECKPOINT", "Sent checkpoint response to", targetPeer.String(), "checkpoint=", resp.Checkpoint)
}

func (ln *Libp2pNetwork) handleCheckpointStream(s network.Stream) {
	defer s.Close()
	var resp CheckpointHashResponse
	decoder := json.NewDecoder(s)
	if err := decoder.Decode(&resp); err != nil {
		logx.Error("NETWORK:CHECKPOINT", "Failed to decode checkpoint response:", err)
		return
	}

	logx.Info("NETWORK:CHECKPOINT", "Received checkpoint response from", resp.PeerID, "checkpoint=", resp.Checkpoint)

	// Compare with local checkpoint
	_, localHash, ok := ln.getCheckpointHash(resp.Checkpoint)
	if !ok {
		logx.Warn("NETWORK:CHECKPOINT", "Local missing checkpoint block", resp.Checkpoint)
		return
	}
	if localHash != resp.BlockHash {
		logx.Warn("NETWORK:CHECKPOINT", "Checkpoint hash mismatch at", resp.Checkpoint, "from peer", resp.PeerID)
		// Re-request exact block if mismatch
		ctx := context.Background()
		if err := ln.RequestSingleBlockSync(ctx, resp.Checkpoint); err != nil {
			logx.Error("NETWORK:CHECKPOINT", "Failed to request single block sync:", err)
		}
	}
}

func (ln *Libp2pNetwork) RequestCheckpointHash(ctx context.Context, checkpoint uint64) error {
	logx.Info("NETWORK:CHECKPOINT", "Broadcasting checkpoint request checkpoint=", checkpoint)
	req := CheckpointHashRequest{
		RequesterID: ln.host.ID().String(),
		Checkpoint:  checkpoint,
		Addrs:       ln.host.Addrs(),
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if ln.topicCheckpointRequest == nil {
		return fmt.Errorf("checkpoint request topic not initialized")
	}
	return ln.topicCheckpointRequest.Publish(ctx, data)
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

// checkNetworkHasSnapshot checks if any peer in the network has snapshots
func (ln *Libp2pNetwork) checkNetworkHasSnapshot() bool {
	// For now, assume network has snapshots if we have peers
	// In a real implementation, you might want to query peers for snapshot availability
	peerCount := len(ln.host.Network().Peers())
	if peerCount == 0 {
		// No peers, this is likely the first node
		return false
	}
	// Has peers, assume network has snapshots
	return true
}

// requestSnapshotOnJoin sends a snapshot request when node joins the network
func (ln *Libp2pNetwork) requestSnapshotOnJoin() {
	if ln.topicSnapshotRequest == nil {
		logx.Info("SNAPSHOT:REQUEST", "request topic not ready; skip request")
		return
	}

	// Check if we already have a snapshot
	path := snapshot.GetSnapshotPath()
	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
		logx.Info("SNAPSHOT:REQUEST", "already have snapshot, skip request")
		return
	}

	// Send snapshot request
	req := SnapshotRequest{
		PeerID:       ln.selfPubKey,
		WantSlot:     0, // Request latest snapshot
		ReceiverAddr: ln.getAnnounceUDPAddr(),
		ChunkSize:    16384,
	}
	data, _ := json.Marshal(req)
	if err := ln.topicSnapshotRequest.Publish(ln.ctx, data); err == nil {
		logx.Info("SNAPSHOT:REQUEST", "snapshot request sent on join")
	} else {
		logx.Error("SNAPSHOT:REQUEST", "failed to send snapshot request:", err)
	}
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
				// Use single snapshot directory
				path := snapshot.GetSnapshotPath()
				fi, err := os.Stat(path)
				if err != nil {
					logx.Info("SNAPSHOT:GOSSIP", "no snapshot-latest.json found")
					// Don't mark ready yet, wait for snapshot download and block sync
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
				if ann.UDPAddr == "" {
					// skip announce if no valid addr
					continue
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

	if ip == "" {
		logx.Error("SNAPSHOT:GOSSIP", "no valid non-loopback IP found; skip announce")
		return ""
	}
	addr := fmt.Sprintf("%s:%d", ip, 9100)
	logx.Info("SNAPSHOT:GOSSIP", "announce UDP addr", addr)
	return addr
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
			// Call the callback function that was set up in SetupCallbacks
			if ln.onSnapshotAnnounce != nil {
				if err := ln.onSnapshotAnnounce(ann); err != nil {
					logx.Error("SNAPSHOT:GOSSIP", "Error in OnSnapshotAnnounce callback:", err)
				}
			} else {
				logx.Warn("SNAPSHOT:GOSSIP", "OnSnapshotAnnounce callback not set")
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

		// Check snapshot availability in single directory
		path := snapshot.GetSnapshotPath()
		fi, err := os.Stat(path)
		if err != nil {
			// no snapshot to announce
			logx.Info("SNAPSHOT:GOSSIP", "request received but no snapshot found")
			continue
		}

		snap, err := snapshot.ReadSnapshot(path)
		if err != nil || snap == nil {
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

// SetJoinBehavior sets whether the node should join the network immediately or wait for sync
func (ln *Libp2pNetwork) SetJoinBehavior(joinAfterSync bool) {
	ln.joinAfterSync = joinAfterSync
	logx.Info("NETWORK:JOIN", "Join behavior set:", "joinAfterSync=", joinAfterSync)
}
