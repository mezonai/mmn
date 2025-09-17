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
	"github.com/mezonai/mmn/utils"
)

func (ln *Libp2pNetwork) SetupCallbacks(ld *ledger.Ledger, privKey ed25519.PrivateKey, self config.NodeConfig, bs store.BlockStore, collector *consensus.Collector, mp *mempool.Mempool, recorder *poh.PohRecorder, snapshotUDPPort string) {
	// Set snapshot UDP port
	ln.snapshotUDPPort = snapshotUDPPort

	latestSlot := bs.GetLatestFinalizedSlot()
	ln.SetNextExpectedSlot(latestSlot + 1)

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

			// Reset poh to sync poh clock with leader
			// if blk.Slot > bs.GetLatestStoreSlot() {
			// 	logx.Info("BLOCK", fmt.Sprintf("Resetting poh clock with leader at slot %d", blk.Slot))
			// 	if err := ln.OnSyncPohFromLeader(blk.LastEntryHash(), blk.Slot); err != nil {
			// 		logx.Error("BLOCK", "Failed to sync poh from leader: ", err)
			// 	}
			// }

			if err := bs.AddBlockPending(blk); err != nil {
				logx.Error("BLOCK", "Failed to store block: ", err)
				return err
			}

			// Remove transactions in block from mempool and add tx tracker if node is follower
			if self.PubKey != blk.LeaderID {
				go mp.BlockCleanup(blk)
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
		OnEmptyBlockReceived: func(blocks []*block.BroadcastedBlock) error {
			logx.Info("EMPTY_BLOCK", "Processing", len(blocks), "empty blocks")

			for _, blk := range blocks {
				if blk == nil {
					continue
				}

				if existingBlock := bs.Block(blk.Slot); existingBlock != nil {
					continue
				}

				if err := bs.AddBlockPending(blk); err != nil {
					logx.Error("EMPTY_BLOCK", "Failed to save empty block to store:", err)
					continue
				}

				if err := ld.ApplyBlock(utils.BroadcastedBlockToBlock(blk)); err != nil {
					logx.Error("EMPTY_BLOCK", "Failed to apply empty block to ledger:", err)
					continue
				}
			}

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
				logx.Info("VOTE", "Committed vote from OnVote Received: slot= ", vote.Slot, ",voter= ", vote.VoterID)
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
				logx.Error("NETWORK: SYNC TRANS", "Failed to add transaction from P2P to mempool: ", err)
			}
			return nil
		},
		OnSyncResponseReceived: func(blocks []*block.BroadcastedBlock) error {
			logx.Info("NETWORK:SYNC BLOCK", "Processing ", len(blocks), " blocks from sync response")

			// Add all blocks to global ordering queue
			for _, blk := range blocks {
				if blk == nil {
					continue
				}

				// Add to ordering queue - this will process blocks in order
				if err := ln.AddBlockToOrderingQueue(blk, bs, ld); err != nil {
					logx.Error("NETWORK:SYNC BLOCK", "Failed to add block to ordering queue: ", err)
					continue
				}
			}

			if !ln.IsNodeReady() {
				gap := uint64(0)
				if ln.worldLatestSlot > blocks[len(blocks)-1].Slot {
					gap = ln.worldLatestSlot - blocks[len(blocks)-1].Slot
				}

				if gap <= SnapshotReadyGapThreshold {
					ln.enableFullModeOnce.Do(func() {
						ln.SetupPubSubTopics(ln.ctx)
						ln.setNodeReady()
					})
				}
			}

			return nil
		},
		OnLatestSlotReceived: func(latestSlot uint64, peerID string) error {
			logx.Info("world Latest Slot", "data: ", latestSlot, "peerId", peerID)
			ln.worldLatestSlot = latestSlot
			return nil
		},
		OnSnapshotAnnounce: func(ann SnapshotAnnounce) error {
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
			localSlot := ln.blockStore.GetLatestFinalizedSlot()
			if ann.Slot <= localSlot+SnapshotReadyGapThreshold {
				logx.Info("SNAPSHOT:DOWNLOAD", "skip announce; local at/above slot", "local=", localSlot, "ann=", ann.Slot)
				return nil
			}
			// Check if we already have a snapshot >= announced slot
			path := snapshot.GetSnapshotPath()
			if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
				if snap, err := snapshot.ReadSnapshot(path); err == nil && snap != nil {
					if snap.Meta.Slot >= ann.Slot {
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

			// Ensure snapshot directory exists before creating downloader
			if err := snapshot.EnsureSnapshotDirectory(); err != nil {
				logx.Error("SNAPSHOT:DOWNLOAD", "Failed to ensure snapshot directory:", err)
				return nil
			}

			// Use single snapshot directory
			down := snapshot.NewSnapshotDownloader(accountStore, snapshot.SnapshotDirectory)
			logx.Info("SNAPSHOT:DOWNLOAD", "start", ann.UDPAddr)
			go func() {
				logx.Info("SNAPSHOT:DEBUG", "Starting download from peer: ", ann.PeerID, " UDP: ", ann.UDPAddr, " Slot: ", ann.Slot)
				maxRetries := 3
				backoff := 2 * time.Second
				for attempt := 1; attempt <= maxRetries; attempt++ {
					task, err := down.DownloadSnapshotFromPeer(ln.ctx, ann.UDPAddr, ann.PeerID, ann.Slot, ann.ChunkSize)
					if err != nil {
						logx.Error("SNAPSHOT:DOWNLOAD", "start failed:", err, " attempt:", attempt, "/", maxRetries)
						if attempt == maxRetries {
							panic(fmt.Sprintf("snapshot download failed after %d attempts (peer=%s udp=%s slot=%d): %v", maxRetries, ann.PeerID, ann.UDPAddr, ann.Slot, err))
						}
						time.Sleep(backoff)
						backoff *= 2
						continue
					}
					for {
						time.Sleep(2 * time.Second)
						st, ok := down.GetDownloadStatus(task.ID)
						if !ok || st == nil {
							continue
						}
						if st.Status == snapshot.TransferStatusComplete {
							logx.Info("SNAPSHOT:DOWNLOAD", "completed ", ann.UDPAddr)
							ln.SetNextExpectedSlot(ann.Slot + 1)

							logx.Info("SNAPSHOT:DOWNLOAD", "Snapshot loaded, starting block sync")
							// Trigger a single block sync from the announced slot + 1
							go func() {
								ctx := context.Background()
								if err := ln.RequestBlockSync(ctx, ann.Slot+1); err != nil {
									logx.Error("NETWORK:SYNC BLOCK", "Failed to request block sync after snapshot:", err)
									return
								}
								logx.Info("NETWORK:SYNC BLOCK", "Block sync requested from slot ", ann.Slot+1)
							}()
							return
						}
						if st.Status == snapshot.TransferStatusFailed || st.Status == snapshot.TransferStatusCancelled {
							logx.Error("SNAPSHOT:DOWNLOAD", "not successful:", st.Status, " attempt:", attempt, "/", maxRetries)
							if attempt == maxRetries {
								panic(fmt.Sprintf("snapshot transfer %s after %d attempts (peer=%s udp=%s slot=%d)", st.Status, maxRetries, ann.PeerID, ann.UDPAddr, ann.Slot))
							}
							// retry with backoff
							time.Sleep(backoff)
							backoff *= 2
							break
						}
					}
				}
			}()
			return nil
		}})

	// Start UDP snapshot streamer for serving nodes
	accountStore := ld.GetAccountStore()
	if accountStore != nil {
		go func() {
			// Ensure snapshot directory exists before starting streamer
			if err := snapshot.EnsureSnapshotDirectory(); err != nil {
				logx.Error("SNAPSHOT:STREAMER", "Failed to ensure snapshot directory:", err)
				return
			}

			// Use single snapshot directory
			if err := snapshot.StartSnapshotUDPStreamer(snapshot.SnapshotDirectory, snapshotUDPPort); err != nil {
				logx.Error("SNAPSHOT:STREAMER", "failed to start:", err)
			}
		}()
	}

	// Temporary comment to save bandwidth for main flow
	// go ln.startPeriodicSyncCheck(bs)

	// Start continuous gap detection
	// Temporary comment to save bandwidth for main flow
	// go ln.startContinuousGapDetection(bs)

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

	go writeSnapshotIfDue(ld, vote.Slot)

	logx.Info("VOTE", "Block finalized via P2P! slot=", vote.Slot)
	return nil
}

func (ln *Libp2pNetwork) SetupPubSubSyncTopics(ctx context.Context) {

	if ln.topicBlockSyncReq == nil {
		if topic, err := ln.pubsub.Join(BlockSyncRequestTopic); err == nil {
			ln.topicBlockSyncReq = topic
			if sub, err := ln.topicBlockSyncReq.Subscribe(); err == nil {
				exception.SafeGoWithPanic("handleBlockSyncRequestTopic", func() {
					ln.handleBlockSyncRequestTopic(ctx, sub)
				})
			}
		}
	}

	if t, e := ln.pubsub.Join(LatestSlotTopic); e == nil {
		ln.topicLatestSlot = t
		if sub, err := ln.topicLatestSlot.Subscribe(); err == nil {
			exception.SafeGoWithPanic("HandleLatestSlotTopic", func() {
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
		}
	}

	if t, e := ln.pubsub.Join(TopicSnapshotRequest); e == nil {
		ln.topicSnapshotRequest = t
		if sub, e2 := ln.topicSnapshotRequest.Subscribe(); e2 == nil {
			exception.SafeGoWithPanic("HandleSnapshotRequest", func() {
				ln.handleSnapshotRequest(ctx, sub)
			})
		}
	}

	if t, e := ln.pubsub.Join(TopicEmptyBlocks); e == nil {
		ln.topicEmptyBlocks = t
		if sub, e2 := ln.topicEmptyBlocks.Subscribe(); e2 == nil {
			exception.SafeGoWithPanic("TopicEmptyBlocks", func() {
				ln.HandleEmptyBlockTopic(ctx, sub)
			})
		}
	}

	ln.startSnapshotAnnouncer()

	if !ln.joinAfterSync {
		go func() {
			// wait until network has more than 1 peer, max 3 seconds
			startTime := time.Now()
			maxWaitTime := 3 * time.Second

			for {
				peerCount := ln.GetPeersConnected()
				if peerCount > 1 {
					break
				}
				// Check if we've waited too long
				if time.Since(startTime) > maxWaitTime {
					break
				}

			}

			localLatestSlot := ln.blockStore.GetLatestFinalizedSlot()

			if localLatestSlot == 0 {
				ln.SetupPubSubTopics(ln.ctx)
			} else {

				for {
					if ln.worldLatestSlot > 0 {
						break
					}
					time.Sleep(1 * time.Second)
				}
				if localLatestSlot < ln.worldLatestSlot {
					ln.RequestBlockSyncFromLatest(ln.ctx)
				} else {
					ln.SetupPubSubTopics(ln.ctx)
				}
			}
		}()
	} else {
		go ln.requestSnapshotOnJoin()
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

	if t, e := ln.pubsub.Join(CheckpointRequestTopic); e == nil {
		ln.topicCheckpointRequest = t
		if sub, err := ln.topicCheckpointRequest.Subscribe(); err == nil {
			exception.SafeGoWithPanic("HandleCheckpointRequestTopic", func() {
				ln.HandleCheckpointRequestTopic(ctx, sub)
			})
		}
	}
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
	latest := ln.blockStore.GetLatestFinalizedSlot()
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

	_, localHash, ok := ln.getCheckpointHash(resp.Checkpoint)
	if !ok {
		return
	}
	if localHash != resp.BlockHash {
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
	if cbs.OnEmptyBlockReceived != nil {
		ln.onEmptyBlockReceived = cbs.OnEmptyBlockReceived
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

// requestSnapshotOnJoin sends a snapshot request when node joins the network
func (ln *Libp2pNetwork) requestSnapshotOnJoin() {
	// wait network connect
	time.Sleep(6 * time.Second)
	logx.Info("SNAPSHOT:REQUEST", "request snapshot on join")
	if ln.topicSnapshotRequest == nil {
		logx.Info("SNAPSHOT:REQUEST", "request topic not ready; skip request")
		return
	}

	// Send snapshot request
	req := SnapshotRequest{}
	data, _ := json.Marshal(req)
	if err := ln.topicSnapshotRequest.Publish(ln.ctx, data); err == nil {
		logx.Info("SNAPSHOT:REQUEST", "snapshot request sent on join")
	}
}

func (ln *Libp2pNetwork) startSnapshotAnnouncer() {
	if ln.topicSnapshotAnnounce == nil {
		logx.Info("SNAPSHOT:GOSSIP", "announce topic not ready; skip announcer")
		return
	}
	go func() {
		ticker := time.NewTicker(2 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ln.ctx.Done():
				return
			case <-ticker.C:
				path := snapshot.GetSnapshotPath()
				fi, err := os.Stat(path)
				if err != nil {
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
					ChunkSize: SnapshotChunkSize,
					CreatedAt: time.Now().Unix(),
					PeerID:    ln.selfPubKey,
				}
				if ann.UDPAddr == "" {
					continue
				}
				data, _ := json.Marshal(ann)
				if err := ln.topicSnapshotAnnounce.Publish(ln.ctx, data); err == nil {
					logx.Info("SNAPSHOT:GOSSIP", "Announce published slot=", ann.Slot)
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

	port := strings.TrimPrefix(ln.snapshotUDPPort, ":")

	addr := fmt.Sprintf("%s:%s", ip, port)
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
		localSlot := ln.blockStore.GetLatestFinalizedSlot()
		if ann.Slot > localSlot {
			if ln.onSnapshotAnnounce != nil {
				if err := ln.onSnapshotAnnounce(ann); err != nil {
					logx.Error("SNAPSHOT:GOSSIP", "Error in OnSnapshotAnnounce callback:", err)
				}
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

		if msg.ReceivedFrom.String() == ln.host.ID().String() {
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
			ChunkSize: SnapshotChunkSize,
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

func writeSnapshotIfDue(ld *ledger.Ledger, slot uint64) {
	if slot%SnapshotRangeFor != 0 {
		return
	}
	accountStore := ld.GetAccountStore()
	if accountStore == nil {
		return
	}

	// Get all accounts using AccountStore instead of provider
	accounts, err := accountStore.GetAll()
	if err != nil {
		logx.Error("SNAPSHOT", fmt.Sprintf("Failed to get all accounts at slot %d: %v", slot, err))
		return
	}

	bankHash, err := snapshot.ComputeFullBankHashFromAccounts(accounts)
	if err != nil {
		logx.Error("SNAPSHOT", fmt.Sprintf("BankHash compute failed at slot %d: %v", slot, err))
		return
	}
	dir := snapshot.SnapshotDirectory
	saved, err := snapshot.WriteSnapshotFromAccounts(dir, accounts, slot, bankHash, nil)
	if err != nil {
		logx.Error("SNAPSHOT", fmt.Sprintf("Failed to write snapshot at slot %d: %v", slot, err))
		return
	}

	logx.Info("SNAPSHOT", fmt.Sprintf("Created snapshot: %s (slot %d)", saved, slot))
}
