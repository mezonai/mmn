package p2p

import (
	"context"
	"encoding/json"
	"time"

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

func (ln *Libp2pNetwork) handleBlockSyncResponseTopic(parentCtx context.Context, sub *pubsub.Subscription) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	logx.Info("NETWORK:SYNC BLOCK", "Starting sync response topic handler")

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

			if len(resp.Blocks) > 0 {
				firstSlot := resp.Blocks[0].Slot
				lastSlot := resp.Blocks[len(resp.Blocks)-1].Slot
				logx.Info("NETWORK:SYNC BLOCK", "Received", len(resp.Blocks), " blocks from slot ", firstSlot, " to ", lastSlot)
			}

			if ln.onSyncResponseReceived != nil {
				if err := ln.onSyncResponseReceived(resp.Blocks); err != nil {
					logx.Error("NETWORK:SYNC BLOCK", "Failed to process sync response: ", err.Error())
				} else {
					logx.Info("NETWORK:SYNC BLOCK", "Sync response callback completed successfully")
				}
			}

			if len(resp.Blocks) > 0 && ln.onBlockReceived != nil {
				for _, blk := range resp.Blocks {
					if blk != nil {
						ln.onBlockReceived(blk)
					}
				}
				logx.Info("NETWORK:SYNC BLOCK", "All blocks processed through onBlockReceived callback")
			}
		}
	}
}

func (ln *Libp2pNetwork) handleBlockSyncRequestTopic(parentCtx context.Context, sub *pubsub.Subscription) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	const batchSize = 10

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

			logx.Info("NETWORK:SYNC BLOCK", "Received sync request from peer", msg.ReceivedFrom.String(), "for slot", req.FromSlot)

			if boundary, ok := ln.blockStore.LastEntryInfoAtSlot(0); ok {
				logx.Info("NETWORK:SYNC BLOCK", "Block store has blocks up to slot", boundary.Slot)
			} else {
				logx.Info("NETWORK:SYNC BLOCK", "Block store appears to be empty")
			}

			var batch []*block.Block
			slot := req.FromSlot
			totalBlocksFound := 0
			totalBlocksSent := 0

			for {
				select {
				case <-ctx.Done():
					return
				default:
					blk := ln.GetBlock(slot)
					if blk == nil {
						if _, ok := ln.blockStore.LastEntryInfoAtSlot(slot); ok {
							logx.Info("NETWORK:SYNC BLOCK", "Highest slot in store")
						}

						if len(batch) > 0 {
							logx.Info("NETWORK:SYNC BLOCK", "Sending final batch of", len(batch), "blocks, ending at slot", slot-1)
							ln.sendBlockBatch(batch)
							totalBlocksSent += len(batch)
						}
						goto EndCurrentRequest
					}

					totalBlocksFound++
					batch = append(batch, blk)
					if len(batch) >= batchSize {
						logx.Info("NETWORK:SYNC BLOCK", "Sending batch of", len(batch), "blocks, ending at slot", slot, "Total sent so far", totalBlocksSent+len(batch))
						ln.sendBlockBatch(batch)
						totalBlocksSent += len(batch)

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
	if len(blocks) == 0 {
		logx.Debug("NETWORK:SYNC BLOCK", "No blocks to send in batch")
		return
	}

	resp := SyncResponse{Blocks: blocks}
	data, err := json.Marshal(resp)
	if err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "Marshal error:", err.Error())
		return
	}

	// Check if topic is available
	if ln.topicBlockSyncRes == nil {
		logx.Error("NETWORK:SYNC BLOCK", "Sync response topic is not initialized")
		return
	}

	// Publish with retry
	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err = ln.topicBlockSyncRes.Publish(context.Background(), data)
		if err == nil {
			logx.Info("NETWORK:SYNC BLOCK", "Successfully published sync response with", len(blocks), "blocks")
			return
		}

		logx.Warn("NETWORK:SYNC BLOCK", "Failed to publish sync response (attempt", attempt, "/", maxRetries, "):", err.Error())

		if attempt < maxRetries {
			// Wait before retry
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}

	logx.Error("NETWORK:SYNC BLOCK", "Failed to publish sync response after", maxRetries, "attempts")
}
