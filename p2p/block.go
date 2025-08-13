package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"mmn/block"
	"mmn/logx"

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

func (ln *Libp2pNetwork) handleBlockSyncResponseTopic(parentCtx context.Context, sub *pubsub.Subscription) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

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
				logx.Error("NETWORK:SYNC BLOCK", "Next ", err.Error())
				continue
			}
			if msg.ReceivedFrom == ln.host.ID() {
				logx.Debug("NETWORK:SYNC BLOCK", "Skipping sync response from self")
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
}

func (ln *Libp2pNetwork) handleBlockSyncRequestTopic(parentCtx context.Context, sub *pubsub.Subscription) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	const batchSize = 10

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
				logx.Debug("NETWORK:SYNC BLOCK", "Skipping sync request from self")
				continue
			}

			var req SyncRequest
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				logx.Error("NETWORK:SYNC BLOCK", "Failed to unmarshal SyncRequest: ", err.Error())
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
				select {
				case <-ctx.Done():
					return
				default:
					blk := ln.GetBlock(slot)
					if blk == nil {
						logx.Info("NETWORK:SYNC BLOCK", fmt.Sprintf(
							"No block found for slot %d. Ending batch.", slot,
						))

						if boundary, ok := ln.blockStore.LastEntryInfoAtSlot(slot); ok {
							logx.Info("NETWORK:SYNC BLOCK", fmt.Sprintf(
								"Highest slot in store: %d", boundary.Slot,
							))
						} else {
							logx.Info("NETWORK:SYNC BLOCK", "Block store appears empty")
						}

						if len(batch) > 0 {
							logx.Info("NETWORK:SYNC BLOCK", fmt.Sprintf(
								"Sending final batch of %d blocks, ending at slot %d", len(batch), slot-1,
							))
							ln.sendBlockBatch(batch)
						}
						goto EndCurrentRequest
					}

					logx.Info("NETWORK:SYNC BLOCK", fmt.Sprintf(
						"Found block for slot %d, hash=%s",
						slot, blk.Hash,
					))

					batch = append(batch, blk)
					if len(batch) >= batchSize {
						logx.Info("NETWORK:SYNC BLOCK", fmt.Sprintf(
							"Sending batch of %d blocks, ending at slot %d", len(batch), slot,
						))
						ln.sendBlockBatch(batch)
						batch = batch[:0]
					}

					slot++
				}
			}

		EndCurrentRequest:
			continue
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
