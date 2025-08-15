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
	"github.com/libp2p/go-libp2p/core/peer"
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

	bytesWritten, err := s.Write(data)
	if err != nil {
		return err
	}

	logx.Info("NETWORK:SYNC BLOCK", "Successfully wrote batch of", len(batch), "blocks,", bytesWritten, "bytes")
	return nil
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

	localLatestSlot := ln.blockStore.GetLatestSlot()
	logx.Info("NETWORK:SYNC BLOCK", "Starting to send blocks. Local latest slot:", localLatestSlot, "Requested from slot:", req.FromSlot)

	if localLatestSlot > 0 {
		logx.Info("NETWORK:SYNC BLOCK", "Block store has blocks up to slot", localLatestSlot)
	} else {
		return
	}

	var firstAvailableSlot uint64 = req.FromSlot
	for firstAvailableSlot <= localLatestSlot {
		if ln.GetBlock(firstAvailableSlot) != nil {
			break
		}
		firstAvailableSlot++
	}

	if firstAvailableSlot > localLatestSlot {
		logx.Warn("NETWORK:SYNC BLOCK", "No blocks available from slot", req.FromSlot, "to", localLatestSlot)
		return
	}

	var batch []*block.Block
	slot := firstAvailableSlot
	totalBlocksSent := 0

	for slot <= localLatestSlot {
		blk := ln.GetBlock(slot)
		if blk == nil {
			logx.Info("NETWORK:SYNC BLOCK", "No block found at slot", slot, "stopping scan")
			break
		}

		batch = append(batch, blk)
		if len(batch) >= batchSize {
			logx.Info("NETWORK:SYNC BLOCK", "Sending batch of", len(batch), " blocks, ending at slot ", slot, " Total sent so far ", totalBlocksSent+len(batch))
			if err := ln.sendBlockBatchStream(batch, s); err != nil {
				logx.Error("NETWORK:SYNC BLOCK", "Failed to send batch:", err)
				return
			}
			totalBlocksSent += len(batch)
			batch = batch[:0]
		}

		slot++
	}

	if len(batch) > 0 {
		if err := ln.sendBlockBatchStream(batch, s); err != nil {
			logx.Error("NETWORK:SYNC BLOCK", "Failed to send final batch:", err)
			return
		}
		totalBlocksSent += len(batch)
	}

	if err := s.Close(); err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "Error closing stream:", err)
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

			latestSlot := ln.getLocalLatestSlot()

			ln.sendLatestSlotResponse(msg.ReceivedFrom, latestSlot)
		}
	}
}

func (ln *Libp2pNetwork) getLocalLatestSlot() uint64 {
	return ln.blockStore.GetLatestSlot()
}

func (ln *Libp2pNetwork) sendLatestSlotResponse(targetPeer peer.ID, latestSlot uint64) {
	stream, err := ln.host.NewStream(context.Background(), targetPeer, LatestSlotProtocol)
	if err != nil {
		logx.Error("NETWORK:LATEST SLOT", "Failed to open stream to peer:", err)
		return
	}
	defer stream.Close()

	response := LatestSlotResponse{
		LatestSlot: latestSlot,
		PeerID:     ln.host.ID().String(),
	}

	data, err := json.Marshal(response)
	if err != nil {
		logx.Error("NETWORK:LATEST SLOT", "Failed to marshal latest slot response:", err)
		return
	}

	_, err = stream.Write(data)
	if err != nil {
		logx.Error("NETWORK:LATEST SLOT", "Failed to write latest slot response:", err)
		return
	}

}

func (ln *Libp2pNetwork) handleLatestSlotStream(s network.Stream) {
	defer s.Close()
	var response LatestSlotResponse
	decoder := json.NewDecoder(s)
	if err := decoder.Decode(&response); err != nil {
		logx.Error("NETWORK:LATEST SLOT", "Failed to decode latest slot response:", err)
		return
	}

	if ln.onLatestSlotReceived != nil {
		if err := ln.onLatestSlotReceived(response.LatestSlot, response.PeerID); err != nil {
			logx.Error("NETWORK:LATEST SLOT", "Error in latest slot callback:", err)
		}
	}
}
