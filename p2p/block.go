package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"mmn/block"
	"mmn/logx"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
)

func (ln *Libp2pNetwork) HandleBlockTopic(sub *pubsub.Subscription) {
	for {
		msg, err := sub.Next(context.Background())
		if err != nil {
			continue
		}

		var blk *block.Block
		if err := json.Unmarshal(msg.Data, &blk); err != nil {
			continue
		}

		if blk != nil && ln.onBlockReceived != nil {
			ln.onBlockReceived(blk)
		}
	}
}

func (ln *Libp2pNetwork) HandleBlockStream(s network.Stream) {
	logx.Info("NETWORK:BLOCK", "Received block stream")
	defer s.Close()

	buf := make([]byte, 4096)
	n, err := s.Read(buf)
	if err != nil {
		return
	}

	var blk *block.Block
	if err := json.Unmarshal(buf[:n], &blk); err != nil {
		return
	}

	if blk != nil && ln.onBlockReceived != nil {
		ln.onBlockReceived(blk)
	}
}

func (ln *Libp2pNetwork) GetBlock(slot uint64) *block.Block {
	return ln.blockStore.Block(slot)
}

func (ln *Libp2pNetwork) handleBlockSyncResponseTopic(sub *pubsub.Subscription) error {
	for {
		msg, err := sub.Next(context.Background())
		if err != nil {
			logx.Error("NETWORK:SYNC BLOCK", "Next ", err.Error())
			continue
		}

		var resp SyncResponse
		if err := json.Unmarshal(msg.Data, &resp); err != nil {
			logx.Error("NETWORK:SYNC BLOCK", "Unmarshal ", err.Error())
			continue
		}

		if resp.Block != nil && ln.onBlockReceived != nil {
			ln.onBlockReceived(resp.Block)
		}
	}
}

func (ln *Libp2pNetwork) handleBlockSyncRequestTopic(sub *pubsub.Subscription) {
	for {
		msg, err := sub.Next(context.Background())
		if err != nil {
			logx.Error("NETWORK:SYNC BLOCK", "Next ", err.Error())
			continue
		}

		var req SyncRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			continue
		}

		logx.Info("NETWORK:SYNC BLOCK", fmt.Sprintf("Got sync request from %s for slot %d", msg.ReceivedFrom.String(), req.FromSlot))

		for slot := req.FromSlot; ; slot++ {
			blk := ln.GetBlock(slot)

			if blk == nil {
				logx.Error("NETWORK:SYNC BLOCK", "Nil blk")
				break
			}

			resp := SyncResponse{Block: blk}
			data, err := json.Marshal(resp)
			if err != nil {
				logx.Error("NETWORK:SYNC BLOCK", "Marshal ", err.Error())

				continue
			}
			ln.topicBlockSyncRes.Publish(context.Background(), data)
		}
	}
}
