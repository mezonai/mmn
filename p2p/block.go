package p2p

import (
	"context"
	"encoding/json"

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
	blk := ln.blockStore.Block(slot)
	return blk
}

func (ln *Libp2pNetwork) handleBlockSyncRequestTopic(parentCtx context.Context, sub *pubsub.Subscription) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	logx.Info("NETWORK:SYNC BLOCK", "Starting sync request topic handler")

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
				logx.Error("NETWORK:SYNC BLOCK", "Next ", err.Error())
				continue
			}

			if msg.ReceivedFrom == ln.host.ID() {
				continue
			}

			var req SyncRequest
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				logx.Error("NETWORK:SYNC BLOCK", "Failed to unmarshal SyncRequest: ", err.Error())
				continue
			}

		}
	}
}

func (ln *Libp2pNetwork) HandleLatestSlotTopic(ctx context.Context, sub *pubsub.Subscription) {

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

			var req LatestSlotRequest
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				logx.Error("NETWORK:LATEST SLOT", "Failed to unmarshal LatestSlotRequest:", err)
				continue
			}

		}
	}
}
