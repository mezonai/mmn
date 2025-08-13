package p2p

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/logx"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func (ln *Libp2pNetwork) HandleBlockTopic(ctx context.Context, sub *pubsub.Subscription) {
	for {
		select {
		case <-ctx.Done():
			logx.Info("NETWORK:BLOCK", "Stopping block topic handler")
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logx.Warn("NETWORK:BLOCK", "Next error:", err)
				continue
			}

			if msg.ReceivedFrom == ln.host.ID() {
				logx.Debug("NETWORK:BLOCK", "Skipping block message from self")
				continue
			}

			var blk *block.Block
			if err := json.Unmarshal(msg.Data, &blk); err != nil {
				logx.Warn("NETWORK:BLOCK", "Unmarshal error:", err)
				continue
			}

			if blk != nil && ln.onBlockReceived != nil {
				ln.onBlockReceived(blk)
			}
		}
	}
}

func (ln *Libp2pNetwork) GetBlock(slot uint64) *block.Block {
	return ln.blockStore.Block(slot)
}

func (ln *Libp2pNetwork) handleBlockSyncResponseTopic(ctx context.Context, sub *pubsub.Subscription) error {
	for {
		select {
		case <-ctx.Done():
			logx.Info("NETWORK:SYNC BLOCK", "Stopping sync response topic handler")
			return nil
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				logx.Warn("NETWORK:SYNC BLOCK", "Next error:", err)
				continue
			}

			// Skip messages from self to avoid processing own messages
			if msg.ReceivedFrom == ln.host.ID() {
				logx.Debug("NETWORK:SYNC BLOCK", "Skipping sync response from self")
				continue
			}

			var resp SyncResponse
			if err := json.Unmarshal(msg.Data, &resp); err != nil {
				logx.Warn("NETWORK:SYNC BLOCK", "Unmarshal error:", err)
				continue
			}

			if resp.Block != nil && ln.onBlockReceived != nil {
				ln.onBlockReceived(resp.Block)
			}
		}
	}
}

func (ln *Libp2pNetwork) handleBlockSyncRequestTopic(ctx context.Context, sub *pubsub.Subscription) {
	for {
		select {
		case <-ctx.Done():
			logx.Info("NETWORK:SYNC BLOCK", "Stopping sync request topic handler")
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logx.Warn("NETWORK:SYNC BLOCK", "Next error:", err)
				continue
			}

			// Skip messages from self to avoid processing own messages
			if msg.ReceivedFrom == ln.host.ID() {
				logx.Debug("NETWORK:SYNC BLOCK", "Skipping sync request from self")
				continue
			}

			var req SyncRequest
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				logx.Warn("NETWORK:SYNC BLOCK", "Unmarshal error:", err)
				continue
			}

			logx.Info("NETWORK:SYNC BLOCK",
				fmt.Sprintf("Got sync request from %s for slot %d",
					msg.ReceivedFrom.String(), req.FromSlot))

			for slot := req.FromSlot; ; slot++ {
				select {
				case <-ctx.Done():
					return
				default:
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

					if err := ln.topicBlockSyncRes.Publish(ctx, data); err != nil {
						logx.Warn("NETWORK:SYNC BLOCK", "Publish error:", err)
					}
				}
			}
		}
	}
}
