package p2p

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/logx"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
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

			stream, err := ln.host.NewStream(context.Background(), msg.ReceivedFrom, RequestBlockSyncStream)
			if err != nil {
				logx.Error("NETWORK:SYNC BLOCK", "Failed to open stream to peer:", err)
				continue
			}
			defer stream.Close()

			ln.sendBlocksOverStream(req, stream)

			continue
		}
	}
}

func (ln *Libp2pNetwork) handleBlockSyncRequestStream(s network.Stream) {
	defer s.Close()
	ln.streamMu.Lock()
	if len(ln.syncStreams) > 1 {
		ln.streamMu.Unlock()
		s.Close()
		return
	}

	ln.syncStreams[s.Conn().RemotePeer()] = s
	ln.streamMu.Unlock()

	logx.Info("NETWORK:SYNC BLOCK", "Starting sync response stream handler from peer", s.Conn().RemotePeer().String())

	decoder := json.NewDecoder(s)
	batchCount := 0
	totalBlocks := 0

	for {
		var blocks []*block.Block
		if err := decoder.Decode(&blocks); err != nil {
			if errors.Is(err, io.EOF) {
				logx.Info("NETWORK:SYNC BLOCK", "Stream closed by peer after ", batchCount, " batches, ", totalBlocks, " blocks received")
				break
			}
			logx.Error("NETWORK:SYNC BLOCK", "Failed to decode blocks array: ", err.Error())
			break
		}

		batchCount++
		totalBlocks += len(blocks)

		if len(blocks) > 0 {
			firstSlot := blocks[0].Slot
			lastSlot := blocks[len(blocks)-1].Slot
			logx.Info("NETWORK:SYNC BLOCK",
				"Received batch ", batchCount,
				" with ", len(blocks), " blocks from slot ", firstSlot, " to ", lastSlot,
			)
		} else {
			logx.Warn("NETWORK:SYNC BLOCK", "Received empty batch", batchCount)
		}

		if ln.onSyncResponseReceived != nil {
			if err := ln.onSyncResponseReceived(blocks); err != nil {
				logx.Error("NETWORK:SYNC BLOCK", "Failed to process sync response: ", err.Error())
			} else {
				logx.Info("NETWORK:SYNC BLOCK", "Sync response callback completed successfully for batch ", batchCount)
			}
		}

		if len(blocks) > 0 && ln.onBlockReceived != nil {
			for _, blk := range blocks {
				if blk != nil {
					ln.onBlockReceived(blk)
				}
			}
		}
	}
}

func (ln *Libp2pNetwork) sendBlockBatchStream(batch []*block.Block, s network.Stream) error {
	data, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	_, err = s.Write(data)
	return err
}

func (ln *Libp2pNetwork) RequestBlockSyncStream() error {
	time.Sleep(15 * time.Second)

	ctx := context.Background()
	logx.Info("REQUEST BLOCK SYNC")

	peers := ln.host.Network().Peers()

	if len(peers) < 2 {
		logx.Warn("NETWORK:SYNC", "Not enough peers to request block sync")
		return fmt.Errorf("not enough peers")
	}

	targetPeer := peers[1]

	info := ln.host.Peerstore().PeerInfo(targetPeer)

	if err := ln.host.Connect(ctx, info); err != nil {
		logx.Error("NETWORK:SETUP", "connect peer", err.Error())
		return err
	}

	logx.Info("NETWORK:SYNC", "Connected to peer", targetPeer.String())
	return nil
}

func (ln *Libp2pNetwork) sendBlocksOverStream(req SyncRequest, s network.Stream) {
	const batchSize = 10

	if boundary, ok := ln.blockStore.LastEntryInfoAtSlot(0); ok {
		logx.Info("NETWORK:SYNC BLOCK", "Block store has blocks up to slot", boundary.Slot)
	}

	var batch []*block.Block
	slot := req.FromSlot
	totalBlocksSent := 0

	for {
		blk := ln.GetBlock(slot)
		if blk == nil {
			if len(batch) > 0 {
				logx.Info("NETWORK:SYNC BLOCK", "Sending final batch of ", len(batch), " blocks, ending at slot ", slot-1)
				ln.sendBlockBatchStream(batch, s)
				totalBlocksSent += len(batch)
			}
			break
		}

		batch = append(batch, blk)
		if len(batch) >= batchSize {
			logx.Info("NETWORK:SYNC BLOCK", "Sending batch of", len(batch), " blocks, ending at slot ", slot, " Total sent so far ", totalBlocksSent+len(batch))
			ln.sendBlockBatchStream(batch, s)
			totalBlocksSent += len(batch)
			batch = batch[:0]
		}

		slot++
	}
}
