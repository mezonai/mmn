package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"mmn/block"
	"mmn/logx"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func (ln *Libp2pNetwork) HandleBlockTopic(sub *pubsub.Subscription) {
	for {
		logx.Info("NETWORK:BLOCK", "Received block topic")
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

		if len(resp.Blocks) > 0 && ln.onBlockReceived != nil {
			for _, blk := range resp.Blocks {
				if blk != nil {
					ln.onBlockReceived(blk)
				}
			}
		}
	}
}

func (ln *Libp2pNetwork) handleBlockSyncRequestTopic(sub *pubsub.Subscription) {
	const batchSize = 10

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

		logx.Info("NETWORK:SYNC BLOCK", fmt.Sprintf(
			"Got sync request from %s for slot %d",
			msg.ReceivedFrom.String(),
			req.FromSlot,
		))

		var batch []*block.Block
		slot := req.FromSlot

		for {
			blk := ln.GetBlock(slot)
			if blk == nil {
				logx.Info("NETWORK:SYNC BLOCK", "Reached end of available blocks")
				break
			}

			batch = append(batch, blk)

			// If batch is full, send it
			if len(batch) >= batchSize {
				ln.sendBlockBatch(batch)
				batch = batch[:0] // reset batch
			}

			slot++
		}

		if len(batch) > 0 {
			ln.sendBlockBatch(batch)
		}
	}
}

func (ln *Libp2pNetwork) sendBlockBatch(blocks []*block.Block) {
	resp := SyncResponse{Blocks: blocks}
	data, err := json.Marshal(resp)
	if err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "Marshal ", err.Error())
		return
	}
	ln.topicBlockSyncRes.Publish(context.Background(), data)
}
