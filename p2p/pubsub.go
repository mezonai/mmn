package p2p

import (
	"context"
	"crypto/ed25519"
	"fmt"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/monitoring"
	"github.com/pkg/errors"

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
	"github.com/mezonai/mmn/types"
)

func (ln *Libp2pNetwork) SetupCallbacks(ld *ledger.Ledger, privKey ed25519.PrivateKey, self config.NodeConfig, bs store.BlockStore, collector *consensus.Collector, mp *mempool.Mempool, recorder *poh.PohRecorder) {
	ln.SetCallbacks(Callbacks{
		OnBlockReceived: func(blk *block.BroadcastedBlock) error {
			if existingBlock := bs.Block(blk.Slot); existingBlock != nil {
				return nil
			}

			leaderPubKey, err := GetLeaderPublicKey(blk.LeaderID)
			if err != nil {
				return errors.Errorf("invalid leader public key: %s", err.Error())
			}

			if !blk.VerifySignature(leaderPubKey) {
				return errors.Errorf("invalid block signature")
			}

			// Verify PoH. If invalid, mark block and continue to process as a failed block
			logx.Info("BLOCK", "VerifyPoH: verifying PoH for block=", blk.Hash)
			if err := blk.VerifyPoH(); err != nil {
				logx.Error("BLOCK", "Invalid PoH, marking block as InvalidPoH and continuing:", err)
				blk.InvalidPoH = true
				monitoring.IncreaseInvalidPohCount()
			}

			if err := ln.verifyBlockBankHash(blk, ld, bs); err != nil {
				return errors.Errorf("invalid block bankhash %s", err.Error())
			}

			// Reset poh to sync poh clock with leader
			if blk.Slot > bs.GetLatestStoreSlot() {
				logx.Info("BLOCK", fmt.Sprintf("Resetting poh clock with leader at slot %d", blk.Slot))
				if err := ln.OnSyncPohFromLeader(blk.LastEntryHash(), blk.Slot); err != nil {
					logx.Error("BLOCK", "Failed to sync poh from leader: ", err)
				}
			}

			if err := bs.AddBlockPending(blk); err != nil {
				logx.Error("BLOCK", "Failed to store block: ", err)
				return err
			}

			// Remove transactions in block from mempool and add tx tracker if node is follower
			if self.PubKey != blk.LeaderID && !blk.InvalidPoH {
				mp.BlockCleanup(blk)
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

			voterPubKey, err := GetVoterPublicKey(vote.VoterID)
			if err != nil {
				return errors.Errorf("invalid voter public key: %s", err.Error())
			}

			if !vote.VerifySignature(voterPubKey) {
				return errors.Errorf("invalid vote signature")
			}

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

			if !txData.Verify(mp.GetZkVerify()) {
				return errors.Errorf("invalid transaction signature")
			}

			// Add transaction to mempool
			_, err := mp.AddTx(txData, false)
			if err != nil {
				logx.Error("NETWORK: SYNC TRANS", "Failed to add transaction from P2P to mempool: ", err)
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

				leaderPubKey, err := GetLeaderPublicKey(blk.LeaderID)
				if err != nil {
					logx.Error("NETWORK:SYNC BLOCK", "Failed to get leader public key for synced block:", err.Error())
					continue
				}

				if !blk.VerifySignature(leaderPubKey) {
					logx.Error("NETWORK:SYNC BLOCK", "Invalid block signature for synced block from leader=", blk.LeaderID)
					continue
				}

				// Verify PoH
				if err := blk.VerifyPoH(); err != nil {
					logx.Error("NETWORK:SYNC BLOCK", "Invalid PoH for synced block: ", err)
					monitoring.IncreaseInvalidPohCount()
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

			localLatestSlot := bs.GetLatestFinalizedSlot()
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

	logx.Info("VOTE", "Block finalized via P2P! slot=", vote.Slot)
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
			if err := jsonx.Unmarshal(msg.Data, &req); err != nil {
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
	data, err := jsonx.Marshal(req)
	if err != nil {
		return err
	}
	if ln.topicCheckpointRequest == nil {
		return fmt.Errorf("checkpoint request topic not initialized")
	}
	return ln.topicCheckpointRequest.Publish(ctx, data)
}

func (ln *Libp2pNetwork) verifyBlockBankHash(blk *block.BroadcastedBlock, ld *ledger.Ledger, bs store.BlockStore) error {
	var prevBankHash [32]byte
	if blk.Slot > 0 {
		prevBlock := bs.Block(blk.Slot - 1)
		if prevBlock != nil {
			prevBankHash = prevBlock.BankHash
		}
	}

	accountDeltas := make(map[string]*types.Account)

	for _, entry := range blk.Entries {
		txs := entry.Transactions
		if len(txs) == 0 {
			continue // Skip tick-only entries
		}

		for _, tx := range txs {
			sender, err := ld.GetAccount(tx.Sender)
			if err != nil {
				return fmt.Errorf("failed to get sender account: %w", err)
			}
			if sender == nil {
				sender = &types.Account{Address: tx.Sender, Balance: uint256.NewInt(0), Nonce: 0}
			}

			recipient, err := ld.GetAccount(tx.Recipient)
			if err != nil {
				return fmt.Errorf("failed to get recipient account: %w", err)
			}
			if recipient == nil {
				recipient = &types.Account{Address: tx.Recipient, Balance: uint256.NewInt(0), Nonce: 0}
			}

			state := map[string]*types.Account{
				sender.Address:    sender,
				recipient.Address: recipient,
			}

			if err := ledger.ApplyTransaction(state, tx); err != nil {
				// Skip failed transactions for bankhash calculation
				continue
			}

			accountDeltas[sender.Address] = state[sender.Address]
			accountDeltas[recipient.Address] = state[recipient.Address]
		}
	}

	return blk.VerifyBankHash(prevBankHash, accountDeltas)
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
