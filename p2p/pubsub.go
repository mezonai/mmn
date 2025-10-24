package p2p

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/monitoring"

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
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/zkverify"
)

func (ln *Libp2pNetwork) SetupCallbacks(ld *ledger.Ledger, privKey ed25519.PrivateKey, self config.NodeConfig, bs store.BlockStore, collector *consensus.Collector, mp *mempool.Mempool, dedupService *mempool.DedupService, recorder *poh.PohRecorder, zkVerify *zkverify.ZkVerify) {
	// Store zkVerify for transaction verification
	ln.zkVerify = zkVerify

	latestSlot := bs.GetLatestFinalizedSlot()
	ln.SetNextExpectedSlot(latestSlot + 1)

	ln.SetCallbacks(Callbacks{
		OnBlockReceived: func(blk *block.BroadcastedBlock) error {
			if existingBlock := bs.Block(blk.Slot); existingBlock != nil {
				return nil
			}

			if !ln.leaderSchedule.IsLeaderAt(blk.Slot, blk.LeaderID) {
				logx.Error("BLOCK", fmt.Sprintf("Not leader at slot %d, leaderID: %s", blk.Slot, blk.LeaderID))
				return fmt.Errorf("invalid leader")
			}

			if !blk.VerifySignature() {
				logx.Error("BLOCK", fmt.Sprintf("Invalid signature at slot %d, leaderID: %s", blk.Slot, blk.LeaderID))
				return fmt.Errorf("invalid signature")
			}

			if err := ln.verifyBlockTransactions(blk); err != nil {
				logx.Error("BLOCK", fmt.Sprintf("Transaction verification failed at slot %d: %v", blk.Slot, err))
				return fmt.Errorf("transaction verification failed: %w", err)
			}

			if err := blk.VerifyPoH(); err != nil {
				logx.Error("BLOCK", "Invalid PoH, marking block as InvalidPoH and continuing:", err)
				blk.InvalidPoH = true
				monitoring.IncreaseInvalidPohCount()
			}

			var txs []*transaction.Transaction
			var txHashSet = make(map[string]struct{})
			var txDedupHashes []string
			for _, entry := range blk.Entries {
				for _, tx := range entry.Transactions {
					txs = append(txs, tx)
					txHashSet[tx.Hash()] = struct{}{}
					txDedupHashes = append(txDedupHashes, tx.DedupHash())
				}

			}

			if err := mp.VerifyBlockTransactions(txs); err != nil {
				logx.Error("BLOCK", fmt.Sprintf("Block transaction validation failed at slot %d: %v", blk.Slot, err))
				return fmt.Errorf("block transaction validation failed: %w", err)
			}

			if err := bs.AddBlockPending(blk); err != nil {
				logx.Error("BLOCK", "Failed to store block: ", err)
				return err
			}

			// Reset poh to sync poh clock with leader
			if blk.Slot > bs.GetLatestStoreSlot() {
				if err := ln.OnSyncPohFromLeader(blk.LastEntryHash(), blk.Slot); err != nil {
					logx.Error("BLOCK", "Failed to sync poh from leader: ", err)
				}
			}

			if ln.isListener {
				return nil
			}

			// Remove transactions in block from mempool and add tx tracker if block is valid
			if !blk.InvalidPoH {
				dedupService.Add(blk.Slot, txDedupHashes)
				mp.BlockCleanup(blk.Slot, txHashSet)
			}

			vote := &consensus.Vote{
				Slot:      blk.Slot,
				BlockHash: blk.Hash,
				VoterID:   self.PubKey,
			}
			vote.Sign(privKey)

			if err := ln.ProcessVote(ln.blockStore, ld, mp, vote, collector); err != nil {
				return err
			}

			if err := ln.BroadcastVote(ln.ctx, vote); err != nil {
				return err
			}

			return nil
		},
		OnEmptyBlockReceived: func(blocks []*block.BroadcastedBlock) error {
			logx.Info("EMPTY_BLOCK", "Processing", len(blocks), "empty blocks")

			for _, blk := range blocks {
				if blk == nil {
					continue
				}

				if existingBlock := bs.HasCompleteBlock(blk.Slot); existingBlock {
					continue
				}

				if err := bs.AddBlockPending(blk); err != nil {
					logx.Error("EMPTY_BLOCK", "Failed to save empty block to store:", err)
					continue
				}
			}

			return nil
		},
		OnVoteReceived: func(vote *consensus.Vote) error {
			logx.Info("VOTE", "Received vote from network: slot= ", vote.Slot, ",voter= ", vote.VoterID)

			if err := ln.ProcessVote(ln.blockStore, ld, mp, vote, collector); err != nil {
				return err
			}

			return nil
		},
		OnTransactionReceived: func(txData *transaction.Transaction) error {
			logx.Debug("TX", "Processing received transaction from P2P network")

			// Add transaction to mempool
			_, err := mp.AddTx(txData, false)
			if err != nil {
				logx.Error("NETWORK: SYNC TRANS", "Failed to add transaction from P2P to mempool: ", err)
			}
			return nil
		},
		OnSyncResponseReceived: func(blk *block.BroadcastedBlock) error {

			// Add block to global ordering queue
			if blk == nil {
				return nil
			}

			// Add to ordering queue - this will process block in order
			latestProcessed, err := ln.AddBlockToOrderingQueue(blk, bs, ld)
			if err != nil {
				logx.Error("NETWORK:SYNC BLOCK", "Failed to add block to ordering queue: ", err)
				return nil
			}

			if !ln.IsNodeReady() && latestProcessed != nil {
				gap := uint64(0)
				if ln.worldLatestSlot > latestProcessed.Slot {
					gap = ln.worldLatestSlot - latestProcessed.Slot
				}

				if gap <= ReadyGapThreshold {
					logx.Info("NETWORK:SYNC BLOCK", fmt.Sprintf("Gap is less than or equal to ready gap threshold, gap: %d", gap))
					ln.enableFullModeOnce.Do(func() {
						ln.OnForceResetPOH(latestProcessed.LastEntryHash(), latestProcessed.Slot)
						ln.startCoreServices(ln.ctx, true)
					})
				}
			}

			return nil
		},
		OnLatestSlotReceived: func(latestSlot uint64, latestPohSlot uint64, peerID string) error {
			if ln.worldLatestSlot < latestSlot {
				logx.Info("NETWORK:LATEST SLOT", "data: ", latestSlot, "peerId", peerID)
				ln.worldLatestSlot = latestSlot
			}

			if ln.worldLatestPohSlot < latestPohSlot {
				logx.Info("NETWORK:LATEST POH SLOT", "data: ", latestPohSlot, "peerId", peerID)
				ln.worldLatestPohSlot = latestPohSlot
			}

			return nil
		},
	})

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

	// Mark block as finalized
	if err := bs.MarkFinalized(vote.Slot); err != nil {
		return fmt.Errorf("mark block as finalized error: %w", err)
	}

	mp.SetCurrentSlot(vote.Slot)

	if err := ld.ApplyBlock(block, ln.isListener); err != nil {
		return fmt.Errorf("apply block error: %w", err)
	}

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

	if t, e := ln.pubsub.Join(TopicEmptyBlocks); e == nil {
		ln.topicEmptyBlocks = t
		if sub, e2 := ln.topicEmptyBlocks.Subscribe(); e2 == nil {
			exception.SafeGoWithPanic("TopicEmptyBlocks", func() {
				ln.HandleEmptyBlockTopic(ctx, sub)
			})
		}
	}

	if ln.leaderSchedule.Len() == 1 && !ln.isListener {
		ln.startImmediatelyFromLocalLatestSlot()
		return
	}

	exception.SafeGo("WaitPeersAndStart", func() {
		ln.startAfterSyncWithPeers(ctx)
	})
}

func (ln *Libp2pNetwork) startImmediatelyFromLocalLatestSlot() {
	latest := ln.blockStore.GetLatestFinalizedSlot()
	var seed [32]byte
	if latest > 0 {
		if blk := ln.blockStore.Block(latest); blk != nil {
			seed = blk.LastEntryHash()
		}
	}
	if ln.OnForceResetPOH != nil {
		ln.OnForceResetPOH(seed, latest)
	}
	ln.startCoreServices(ln.ctx, true)
}

func (ln *Libp2pNetwork) startAfterSyncWithPeers(ctx context.Context) {
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
		if ln.worldLatestSlot == 0 {
			if !ln.ensureWorldLatestSlotInitialized(ctx) {
				ln.enableFullModeOnce.Do(func() {
					logx.Info("NETWORK", "No world latest slot discovered; starting PoH/Validator for genesis")
					ln.startCoreServices(ln.ctx, true)
				})
				return
			}

			ln.waitUntilSyncWindowAligned(ctx)
			ln.RequestBlockSyncFromLatest(ln.ctx)
			return
		}

		ln.waitUntilSyncWindowAligned(ctx)
		ln.RequestBlockSyncFromLatest(ln.ctx)
		return
	}

	ln.waitForWorldPohSlot()
	if ln.handlePohResetIfNeeded(localLatestSlot) {
		return
	}

	// Handle node crash, should catchup to world latest slot
	ln.waitUntilSyncWindowAligned(ctx)
	if localLatestSlot < ln.worldLatestSlot {
		logx.Info("NETWORK", "Local latest slot is less than world latest slot, requesting block sync from latest")
		ln.RequestBlockSyncFromLatest(ln.ctx)
	} else {
		// No sync required; start services based on local latest state
		logx.Info("NETWORK", "Local latest slot is greater than or equal to world latest slot, starting PoH/Validator")
		ln.enableFullModeOnce.Do(func() {
			ln.startImmediatelyFromLocalLatestSlot()
		})
	}
}

func (ln *Libp2pNetwork) ensureWorldLatestSlotInitialized(ctx context.Context) bool {
	if ln.worldLatestSlot > 0 {
		return true
	}
	retryCount := 0
	for retryCount < InitRequestLatestSlotMaxRetries {
		if ln.worldLatestSlot > 0 {
			break
		}
		ln.RequestLatestSlotFromPeers(ctx)
		time.Sleep(WaitWorldLatestSlotTimeInterval)
		retryCount++
	}
	return ln.worldLatestSlot > 0
}

// isSyncWindowAligned checks if PoH slot and finalized slot are aligned sufficiently to start syncing
func (ln *Libp2pNetwork) isSyncWindowAligned() bool {
	return ln.worldLatestSlot > 0 &&
		!ln.isLeaderOfSlot(ln.worldLatestSlot) &&
		ln.worldLatestPohSlot > 0 &&
		!ln.isLeaderOfSlot(ln.worldLatestPohSlot) &&
		ln.worldLatestPohSlot-ln.worldLatestSlot <= LatestSlotSyncGapThreshold
}

func (ln *Libp2pNetwork) waitUntilSyncWindowAligned(ctx context.Context) {
	for {
		if ln.isSyncWindowAligned() {
			break
		}
		ln.RequestLatestSlotFromPeers(ctx)
		time.Sleep(WaitWorldLatestSlotTimeInterval)
	}
}

func (ln *Libp2pNetwork) waitForWorldPohSlot() {
	for {
		if ln.worldLatestPohSlot > 0 {
			break
		}
		time.Sleep(WaitWorldLatestSlotTimeInterval)
	}
}

func (ln *Libp2pNetwork) handlePohResetIfNeeded(localLatestSlot uint64) bool {
	if ln.worldLatestPohSlot > 0 {
		if localLatestSlot >= ln.worldLatestPohSlot {
			logx.Info("NETWORK", "Local latest slot is equal to world latest POH slot, forcing reset POH")
			var seed [32]byte
			if blk := ln.blockStore.Block(localLatestSlot); blk != nil {
				seed = blk.LastEntryHash()
			}
			if ln.OnForceResetPOH != nil {
				ln.OnForceResetPOH(seed, localLatestSlot)
			}
			ln.startCoreServices(ln.ctx, true)
			return true
		}
	}
	return false
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

	if !ln.isListener {
		if ln.topicTxs, err = ln.pubsub.Join(TopicTxs); err == nil {
			if sub, err := ln.topicTxs.Subscribe(); err == nil {
				exception.SafeGoWithPanic("HandleTransactionTopic", func() {
					ln.HandleTransactionTopic(ctx, sub)
				})
			}
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

	ln.startRealtimeTopicMonitoring(ctx)
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
			if err := jsonx.Unmarshal(msg.Data, &req); err != nil {
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

	data, err := jsonx.Marshal(resp)
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
	decoder := jsonx.NewDecoder(s)
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
	data, err := jsonx.Marshal(req)
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
}

func (ln *Libp2pNetwork) logTopicStatus() {
	logx.Info("NETWORK:PUBSUB:STATUS", "=== TOPIC STATUS REPORT ===")
	logx.Info("NETWORK:PUBSUB:STATUS", fmt.Sprintf("Node ID: %s", ln.host.ID().String()))
	logx.Info("NETWORK:PUBSUB:STATUS", fmt.Sprintf("Total connected peers: %d", ln.GetPeersConnected()))

	if ln.topicBlocks != nil {
		meshPeers := ln.topicBlocks.ListPeers()
		logx.Info("NETWORK:PUBSUB:STATUS", fmt.Sprintf("Block Topic: ACTIVE (mesh peers: %d)", len(meshPeers)))
		for i, peer := range meshPeers {
			logx.Info("NETWORK:PUBSUB:STATUS", fmt.Sprintf("Block mesh peer %d: %s", i+1, peer.String()))
		}
	} else {
		logx.Error("NETWORK:PUBSUB:STATUS", "Block Topic: NOT INITIALIZED")
	}

	if ln.topicVotes != nil {
		meshPeers := ln.topicVotes.ListPeers()
		logx.Info("NETWORK:PUBSUB:STATUS", fmt.Sprintf("Vote Topic: ACTIVE (mesh peers: %d)", len(meshPeers)))
		for i, peer := range meshPeers {
			logx.Info("NETWORK:PUBSUB:STATUS", fmt.Sprintf("Vote mesh peer %d: %s", i+1, peer.String()))
		}
	} else {
		logx.Error("NETWORK:PUBSUB:STATUS", "Vote Topic: NOT INITIALIZED")
	}

	if ln.topicTxs != nil {
		meshPeers := ln.topicTxs.ListPeers()
		logx.Info("NETWORK:PUBSUB:STATUS", fmt.Sprintf("Transaction Topic: ACTIVE (mesh peers: %d)", len(meshPeers)))
	} else {
		logx.Error("NETWORK:PUBSUB:STATUS", "Transaction Topic: NOT INITIALIZED")
	}

	if ln.topicCheckpointRequest != nil {
		meshPeers := ln.topicCheckpointRequest.ListPeers()
		logx.Info("NETWORK:PUBSUB:STATUS", fmt.Sprintf("Checkpoint Request Topic: ACTIVE (mesh peers: %d)", len(meshPeers)))
	} else {
		logx.Error("NETWORK:PUBSUB:STATUS", "Checkpoint Request Topic: NOT INITIALIZED")
	}

	logx.Info("NETWORK:PUBSUB:STATUS", "=== END TOPIC STATUS REPORT ===")
}

func (ln *Libp2pNetwork) startRealtimeTopicMonitoring(ctx context.Context) {
	logx.Info("NETWORK:PUBSUB:MONITOR", "Starting realtime topic monitoring...")

	exception.SafeGoWithPanic("RealtimeTopicMonitoring", func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				ln.logTopicStatus()

				ln.logMeshChanges()

			case <-ctx.Done():
				logx.Info("NETWORK:PUBSUB:MONITOR", "Stopping realtime topic monitoring")
				return
			}
		}
	})
}

func (ln *Libp2pNetwork) logMeshChanges() {
	if ln.topicBlocks != nil {
		currentMeshPeers := ln.topicBlocks.ListPeers()
		logx.Info("NETWORK:PUBSUB:MESH", fmt.Sprintf("Block topic mesh peers: %d", len(currentMeshPeers)))

		if len(currentMeshPeers) == 0 {
			logx.Info("NETWORK:PUBSUB:MESH", "Block topic mesh is EMPTY - this may cause block propagation issues!")
		}
	}

	if ln.topicVotes != nil {
		currentMeshPeers := ln.topicVotes.ListPeers()
		logx.Info("NETWORK:PUBSUB:MESH", fmt.Sprintf("Vote topic mesh peers: %d", len(currentMeshPeers)))

		if len(currentMeshPeers) == 0 {
			logx.Info("NETWORK:PUBSUB:MESH", "Vote topic mesh is EMPTY - this may cause vote propagation issues!")
		}
	}

	if ln.topicBlocks != nil && ln.topicVotes != nil {
		blockMeshSize := len(ln.topicBlocks.ListPeers())
		voteMeshSize := len(ln.topicVotes.ListPeers())

		if blockMeshSize != voteMeshSize {
			logx.Info("NETWORK:PUBSUB:MESH", fmt.Sprintf("Mesh size mismatch: Block=%d, Vote=%d", blockMeshSize, voteMeshSize))
		}
	}
}
