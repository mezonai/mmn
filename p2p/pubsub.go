package p2p

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/blockstore"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/types"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

func (ln *Libp2pNetwork) handleNodeInfoStream(s network.Stream) {
	defer s.Close()

	buf := make([]byte, 2048)
	n, err := s.Read(buf)
	if err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", "Failed to read from bootstrap: ", err)
		return
	}

	var msg map[string]interface{}
	if err := json.Unmarshal(buf[:n], &msg); err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", "Failed to unmarshal peer info: ", err)
		return
	}

	newPeerIDStr := msg["new_peer_id"].(string)
	newPeerID, err := peer.Decode(newPeerIDStr)
	if err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", "Invalid peer ID: ", newPeerIDStr)
		return
	}

	addrStrs := msg["addrs"].([]interface{})
	var addrs []ma.Multiaddr
	for _, a := range addrStrs {
		maddr, err := ma.NewMultiaddr(a.(string))
		if err == nil {
			addrs = append(addrs, maddr)
		}
	}

	peerInfo := peer.AddrInfo{
		ID:    newPeerID,
		Addrs: addrs,
	}

	err = ln.host.Connect(context.Background(), peerInfo)
	if err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", "Failed to connect to new peer: ", err)
	}
}

func (ln *Libp2pNetwork) SetupCallbacks(ld *ledger.Ledger, privKey ed25519.PrivateKey, self config.NodeConfig, bs blockstore.Store, collector *consensus.Collector, mp *mempool.Mempool) {
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

			if len(ln.host.Network().Peers()) > 0 {
				go ln.checkForMissingBlocksAround(bs, blk.Slot)
			}

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

				// Mark block as finalized
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

				// Mark block as finalized
				if err := bs.MarkFinalized(vote.Slot); err != nil {
					return fmt.Errorf("mark block as finalized error: %w", err)
				}

				logx.Info("VOTE", "Block finalized via P2P! slot=", vote.Slot)
			}

			return nil
		},
		OnTransactionReceived: func(txData *types.Transaction) error {
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

				// Add to block store
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

			localLatestSlot := bs.GetLatestSlot()
			if latestSlot > localLatestSlot {
				fromSlot := localLatestSlot + 1
				logx.Info("NETWORK:SYNC BLOCK", "Peer has higher slot:", latestSlot, "local slot:", localLatestSlot, "requesting sync from slot:", fromSlot)

				ctx := context.Background()
				if err := ln.RequestBlockSync(ctx, fromSlot); err != nil {
					logx.Error("NETWORK:SYNC BLOCK", "Failed to send sync request after latest slot:", err)
				}
			}
			return nil
		},
	})

	go ln.startInitialSync(bs)

	go ln.startPeriodicSyncCheck(bs)

	// Start continuous gap detection
	go ln.startContinuousGapDetection(bs)

	// clean sync request expireds every 1 minute
	go ln.startCleanupRoutine()
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

	if ln.topicCheckpointRequest, err = ln.pubsub.Join(CheckpointRequestTopic); err == nil {
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
	} else {
		logx.Info("NETWORK:CHECKPOINT", "Checkpoint hash matched at", resp.Checkpoint)
	}
}

// RequestCheckpointHash broadcasts a checkpoint hash request (e.g., every 10 slots)
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
}

func (ln *Libp2pNetwork) TxBroadcast(ctx context.Context, tx *types.Transaction) error {
	logx.Info("TX", "Broadcasting transaction to network")
	txData, err := json.Marshal(tx)
	if err != nil {
		return fmt.Errorf("failed to serialize transaction: %w", err)
	}

	if err := ln.topicTxs.Publish(ctx, txData); err != nil {
		return fmt.Errorf("failed to publish transaction: %w", err)
	}

	return nil
}

func (ln *Libp2pNetwork) BroadcastVote(ctx context.Context, vote *consensus.Vote) error {
	msg := VoteMessage{
		Slot:      vote.Slot,
		BlockHash: fmt.Sprintf("%x", vote.BlockHash),
		VoterID:   vote.VoterID,
		Signature: vote.Signature,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	if ln.topicVotes != nil {
		ln.topicVotes.Publish(ctx, data)
	}
	return nil
}

func (ln *Libp2pNetwork) BroadcastBlock(ctx context.Context, blk *block.BroadcastedBlock) error {
	logx.Info("BLOCK", "Broadcasting block: slot=", blk.Slot)

	data, err := json.Marshal(blk)
	if err != nil {
		logx.Error("BLOCK", "Failed to marshal block: ", err)
		return err
	}

	if ln.topicBlocks != nil {
		logx.Info("BLOCK", "Publishing block to pubsub topic")
		if err := ln.topicBlocks.Publish(ctx, data); err != nil {
			logx.Error("BLOCK", "Failed to publish block:", err)
		}
	}
	return nil
}

func (ln *Libp2pNetwork) GetPeersConnected() int {
	return len(ln.peers)
}

func (ln *Libp2pNetwork) cleanupOldSyncRequests() {
	ln.syncMu.Lock()
	defer ln.syncMu.Unlock()

	now := time.Now()
	for requestID, info := range ln.activeSyncRequests {
		if !info.IsActive || now.Sub(info.StartTime) > 5*time.Minute {
			delete(ln.activeSyncRequests, requestID)
		}
	}
}

// periodically send checkpoint probe and cleanup old sync requests
func (ln *Libp2pNetwork) startPeriodicSyncCheck(bs blockstore.Store) {
	// wait network setup
	time.Sleep(10 * time.Second)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ln.cleanupOldSyncRequests()
			// probe checkpoint every tick
			latest := bs.GetLatestSlot()
			if latest >= MaxScanRange {
				checkpoint := (latest / MaxScanRange) * MaxScanRange
				logx.Info("NETWORK:CHECKPOINT", "Probing checkpoint=", checkpoint, "latest=", latest)
				_ = ln.RequestCheckpointHash(context.Background(), checkpoint)
			}
		case <-ln.ctx.Done():
			return
		}
	}
}

func (ln *Libp2pNetwork) startCleanupRoutine() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ln.CleanupExpiredRequests()
			ln.cleanupOldMissingBlocksTracker()
		case <-ln.ctx.Done():
			logx.Info("NETWORK:CLEANUP", "Stopping cleanup routine")
			return
		}
	}
}

func (ln *Libp2pNetwork) startInitialSync(bs blockstore.Store) {
	// wait network setup
	time.Sleep(2 * time.Second)

	ctx := context.Background()

	if _, err := ln.RequestLatestSlotFromPeers(ctx); err != nil {
		logx.Warn("NETWORK:SYNC BLOCK", "Failed to request latest slot from peers:", err)
	}
	// sync from 0
	if err := ln.RequestBlockSync(ctx, 0); err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "Failed to send initial sync request:", err)
	}

	// wait for sync all blocks end before start scan
	time.Sleep(15 * time.Second)
	ln.scanMissingBlocks(bs)
}
