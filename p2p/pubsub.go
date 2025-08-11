package p2p

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"mmn/block"
	"mmn/blockstore"
	"mmn/config"
	"mmn/consensus"
	"mmn/ledger"
	"mmn/logx"
	"mmn/mempool"
	"mmn/types"
	"time"

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
	} else {
		logx.Info("NETWORK:HANDLE NODE INFOR STREAM", "Connected to new peer: ", newPeerID)
	}
}

func (ln *Libp2pNetwork) SetupCallbacks(ld *ledger.Ledger, privKey ed25519.PrivateKey, self config.NodeConfig, bs *blockstore.BlockStore, collector *consensus.Collector, mp *mempool.Mempool) {
	ln.SetCallbacks(
		func(blk *block.Block) error {
			logx.Info("BLOCK", "Received block from network: slot=", blk.Slot)
			if err := ld.VerifyBlock(blk); err != nil {
				logx.Error("BLOCK", "Block verification failed: ", err)
				return err
			}

			if err := bs.AddBlockPending(blk); err != nil {
				logx.Error("BLOCK", "Failed to store block: ", err)
				return err
			}

			vote := &consensus.Vote{
				Slot:      blk.Slot,
				BlockHash: blk.Hash,
				VoterID:   self.PubKey,
			}
			vote.Sign(privKey)

			ln.BroadcastVote(context.Background(), vote)

			return nil
		},

		func(vote *consensus.Vote) error {
			logx.Info("VOTE", "Received vote from network: slot= ", vote.Slot, ",voter= ", vote.VoterID)

			committed, needApply, err := collector.AddVote(vote)
			if err != nil {
				logx.Error("VOTE", "Failed to add vote: ", err)
				return err
			}

			if committed && needApply {
				logx.Info("VOTE", "Block committed: slot=", vote.Slot)
				// Apply block to ledger
				// (Implementation depends on your block structure)
			}

			return nil
		},
		func(txData []types.Transaction) error {
			// TODO implement

			return nil
		},
	)
}

func (ln *Libp2pNetwork) SetupPubSubTopics() {
	var err error

	if ln.topicBlocks, err = ln.pubsub.Join(TopicBlocks); err == nil {
		if sub, err := ln.topicBlocks.Subscribe(); err == nil {
			go ln.HandleBlockTopic(sub)
		}
	}

	if ln.topicVotes, err = ln.pubsub.Join(TopicVotes); err == nil {
		if sub, err := ln.topicVotes.Subscribe(); err == nil {
			go ln.HandleVoteTopic(sub)
		}
	}

	if ln.topicTxs, err = ln.pubsub.Join(TopicTxs); err == nil {
		if sub, err := ln.topicTxs.Subscribe(); err == nil {
			go ln.HandleTxTopic(sub)
		}
	}

	if ln.topicBlockSyncReq, err = ln.pubsub.Join(BlockSyncRequestTopic); err == nil {
		if sub, err := ln.topicBlockSyncReq.Subscribe(); err == nil {
			go ln.handleBlockSyncRequestTopic(sub)
		}
	}

	if ln.topicBlockSyncRes, err = ln.pubsub.Join(BlockSyncResponseTopic); err == nil {
		if sub, err := ln.topicBlockSyncRes.Subscribe(); err == nil {
			go ln.handleBlockSyncResponseTopic(sub)
		}
	}

}

func (ln *Libp2pNetwork) SetCallbacks(
	onBlock func(*block.Block) error,
	onVote func(*consensus.Vote) error,
	onTx func([]types.Transaction) error,
) {
	ln.onBlockReceived = onBlock
	ln.onVoteReceived = onVote
	ln.onTxReceived = onTx
}

func (ln *Libp2pNetwork) BroadcastToStreams(data []byte, streams map[peer.ID]network.Stream) {
	ln.streamMu.RLock()
	defer ln.streamMu.RUnlock()

	for _, stream := range streams {
		if stream != nil {
			stream.Write(data)
		}
	}
}

func (ln *Libp2pNetwork) TxBroadcast(ctx context.Context, tx *types.Transaction) error {
	// Implement

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

	ln.BroadcastToStreams(data, ln.voteStreams)

	return nil
}

func (ln *Libp2pNetwork) BroadcastBlock(ctx context.Context, blk *block.Block) error {
	msg := BlockMessage{
		Slot:      blk.Slot,
		PrevHash:  fmt.Sprintf("%x", blk.PrevHash),
		LeaderID:  blk.LeaderID,
		Timestamp: time.Unix(int64(blk.Timestamp), 0),
		Hash:      fmt.Sprintf("%x", blk.Hash),
		Signature: blk.Signature,
	}

	for _, entry := range blk.Entries {
		msg.Entries = append(msg.Entries, fmt.Sprintf("%x", entry.Hash))
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	if ln.topicBlocks != nil {
		if err := ln.topicBlocks.Publish(ctx, data); err != nil {
			logx.Error("PUBSUB", "Failed to publish block:", err)
		}
	}

	ln.BroadcastToStreams(data, ln.blockStreams)

	return nil
}
