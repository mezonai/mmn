package p2p

import (
	"context"
	"encoding/json"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/logx"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func (ln *Libp2pNetwork) HandleEmptyBlockTopic(ctx context.Context, sub *pubsub.Subscription) {
	logx.Info("NETWORK:EMPTY_BLOCK", "Starting empty block topic handler")
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					logx.Warn("NETWORK:EMPTY_BLOCK", "Next error ctx.Err() :", err)
					return
				}
				logx.Warn("NETWORK:EMPTY_BLOCK", "Next error:", err)
				continue
			}

			var blocks []*block.BroadcastedBlock
			if err := json.Unmarshal(msg.Data, &blocks); err != nil {
				logx.Warn("NETWORK:EMPTY_BLOCK", "Unmarshal error:", err)
				ln.UpdatePeerScore(msg.ReceivedFrom, "invalid_message", nil)
				continue
			}

			if len(blocks) > 0 && ln.onEmptyBlockReceived != nil {
				if msg.ReceivedFrom == ln.host.ID() {
					logx.Debug("NETWORK:EMPTY_BLOCK", "Skipping empty blocks from ourselves")
					continue
				}

				logx.Info("NETWORK:EMPTY_BLOCK", "Received", len(blocks), "empty blocks from peer:", msg.ReceivedFrom.String())
				ln.onEmptyBlockReceived(blocks)
			}
		}
	}
}

func (ln *Libp2pNetwork) BroadcastEmptyBlocks(ctx context.Context, blocks []*block.BroadcastedBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	// Marshal the entire array at once
	data, err := json.Marshal(blocks)
	if err != nil {
		logx.Error("EMPTY_BLOCK", "Failed to marshal empty blocks array:", err)
		return err
	}

	if ln.topicEmptyBlocks != nil {
		if err := ln.topicEmptyBlocks.Publish(ctx, data); err != nil {
			logx.Error("EMPTY_BLOCK", "Failed to publish empty blocks batch:", err)
			return err
		}
		logx.Info("EMPTY_BLOCK", "Successfully published", len(blocks), "empty blocks as batch")
	} else {
		logx.Warn("EMPTY_BLOCK", "Topic not ready, cannot publish", "pending blocks:", len(blocks))
	}

	return nil
}
