package p2p

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/jsonx"
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

			var blk *block.BroadcastedBlock
			if err := jsonx.Unmarshal(msg.Data, &blk); err != nil {
				logx.Warn("NETWORK:BLOCK", "Unmarshal error:", err)
				continue
			}

			if blk != nil && ln.onBlockReceived != nil {
				logx.Info("NETWORK:BLOCK", "Received block from peer:", msg.ReceivedFrom.String(), "slot:", blk.Slot)
				ln.onBlockReceived(blk)
			}
		}
	}
}

func (ln *Libp2pNetwork) GetBlock(slot uint64) *block.Block {
	blk := ln.blockStore.Block(slot)
	return blk
}

func (ln *Libp2pNetwork) handleBlockSyncRequestTopic(ctx context.Context, sub *pubsub.Subscription) {
	logx.Info("NETWORK:SYNC BLOCK", "Starting block sync request topic handler")

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

			var req SyncRequest
			if err := jsonx.Unmarshal(msg.Data, &req); err != nil {
				logx.Error("NETWORK:SYNC BLOCK", "Failed to unmarshal SyncRequest: ", err.Error())
				continue
			}

			// Skip requests from self
			if msg.ReceivedFrom == ln.host.ID() {
				continue
			}

			logx.Info("NETWORK:SYNC BLOCK", "Received sync request:", req.RequestID, "from slot", req.FromSlot, "to slot", req.ToSlot, "from peer:", msg.ReceivedFrom.String())

			// Check if this request is already being handled
			ln.syncTrackerMu.RLock()
			tracker, exists := ln.syncRequests[req.RequestID]
			ln.syncTrackerMu.RUnlock()

			if !exists {
				// new tracker
				tracker = NewSyncRequestTracker(req.RequestID, req.FromSlot, req.ToSlot)
				ln.syncTrackerMu.Lock()
				ln.syncRequests[req.RequestID] = tracker
				ln.syncTrackerMu.Unlock()
				logx.Info("NETWORK:SYNC BLOCK", "Created new tracker for request:", req.RequestID)
			}

			if !tracker.ActivatePeer(msg.ReceivedFrom, nil) {
				continue
			}

			ln.sendBlocksOverStream(req, msg.ReceivedFrom)

		}
	}
}

func (ln *Libp2pNetwork) handleBlockSyncRequestStream(s network.Stream) {
	defer s.Close()

	remotePeer := s.Conn().RemotePeer()

	var syncRequest SyncRequest
	decoder := jsonx.NewDecoder(s)
	if err := decoder.Decode(&syncRequest); err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "Failed to decode sync request from stream:", err)
		s.Close()
		return
	}

	logx.Info("NETWORK:SYNC BLOCK", "Received stream for request: ", syncRequest.RequestID, " from peer: ", remotePeer.String())

	// check request id actived
	ln.syncTrackerMu.Lock()
	tracker, exists := ln.syncRequests[syncRequest.RequestID]
	if !exists {
		// new tracker
		tracker = NewSyncRequestTracker(syncRequest.RequestID, syncRequest.FromSlot, syncRequest.ToSlot)
		ln.syncRequests[syncRequest.RequestID] = tracker
	}

	if !tracker.ActivatePeer(remotePeer, s) {
		ln.syncTrackerMu.Unlock()
		return
	}

	ln.syncTrackerMu.Unlock()

	batchCount := 0
	totalBlocks := 0
	processedBlocks := 0

	for {
		var blocks []*block.BroadcastedBlock
		if err := decoder.Decode(&blocks); err != nil {
			if errors.Is(err, io.EOF) {

				break
			}
			logx.Error("NETWORK:SYNC BLOCK", "Failed to decode blocks array: ", err.Error())
			break
		}

		for _, blk := range blocks {
			if blk == nil {
				continue
			}

			totalBlocks++

			if ln.onSyncResponseReceived != nil {
				if err := ln.onSyncResponseReceived(blk); err != nil {
					logx.Error("NETWORK:SYNC BLOCK", "Failed to process sync response: ", err.Error())
				}
			}

			processedBlocks++
			batchCount++

		}

		// clean blocks array reference
		blocks = nil
	}

	// Close all peer streams and remove tracker
	ln.syncTrackerMu.Lock()
	if tracker, exists := ln.syncRequests[syncRequest.RequestID]; exists {
		tracker.CloseAllPeers()
		delete(ln.syncRequests, syncRequest.RequestID)
	}
	ln.syncTrackerMu.Unlock()

	logx.Info("NETWORK:SYNC BLOCK", "Completed stream for request:", syncRequest.RequestID, "total batches:", batchCount, "total blocks:", totalBlocks, "processed:", processedBlocks)

}

func (ln *Libp2pNetwork) sendBlockBatchStream(batch []*block.Block, s network.Stream) error {
	data, err := jsonx.Marshal(batch)
	if err != nil {
		return err
	}

	bytesWritten, err := s.Write(data)
	if err != nil {
		return err
	}

	logx.Info("NETWORK:SYNC BLOCK", "Successfully wrote batch of", bytesWritten, "bytes", "numBlocks=", len(batch))
	return nil
}

func (ln *Libp2pNetwork) RequestBlockSyncStream() error {
	time.Sleep(15 * time.Second)

	ctx := context.Background()
	logx.Info("REQUEST BLOCK SYNC")

	peers := ln.host.Network().Peers()

	if len(peers) == 0 {
		logx.Warn("NETWORK:SYNC", "Not enough peers to request block sync")
		return fmt.Errorf("not enough peers")
	}

	targetPeer := peers[1]

	info := ln.host.Peerstore().PeerInfo(targetPeer)

	if err := ln.host.Connect(ctx, info); err != nil {
		logx.Error("NETWORK:SETUP", "connect peer", err.Error())
		return err
	}

	return nil
}

func (ln *Libp2pNetwork) sendBlocksOverStream(req SyncRequest, targetPeer peer.ID) {

	stream, err := ln.host.NewStream(context.Background(), targetPeer, RequestBlockSyncStream)
	if err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "Failed to create stream to peer:", targetPeer.String(), "error:", err.Error())
		return
	}
	defer stream.Close()

	encoder := jsonx.NewEncoder(stream)
	if err := encoder.Encode(req); err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "Failed to send request ID:", err)
		return
	}

	// Close when done
	defer func() {
		ln.syncTrackerMu.Lock()
		if tracker, exists := ln.syncRequests[req.RequestID]; exists {
			tracker.CloseRequest()
			delete(ln.syncRequests, req.RequestID)
		}
		ln.syncTrackerMu.Unlock()
	}()

	localLatestSlot := ln.blockStore.GetLatestFinalizedSlot()
	if localLatestSlot > 0 && req.FromSlot > localLatestSlot {
		return
	}

	var batch []*block.Block
	totalBlocksSent := 0

	slot := req.FromSlot
	for slot <= localLatestSlot && slot <= req.ToSlot {
		blk := ln.GetBlock(slot)
		if blk != nil {
			batch = append(batch, blk)
		}

		if len(batch) >= int(SyncBlocksBatchSize) {
			if err := ln.sendBlockBatchStream(batch, stream); err != nil {
				logx.Error("NETWORK:SYNC BLOCK", "Failed to send batch:", err)
				return
			}
			totalBlocksSent += len(batch)
			batch = batch[:0]
		}

		slot++
	}

	if len(batch) > 0 {
		if err := ln.sendBlockBatchStream(batch, stream); err != nil {
			logx.Error("NETWORK:SYNC BLOCK", "Failed to send final batch:", err)
			return
		}
		totalBlocksSent += len(batch)
	}

	// must auto send here if want the request node catch up
	if req.ToSlot < localLatestSlot {
		nextFromSlot := req.ToSlot + 1
		nextToSlot := nextFromSlot + SyncBlocksBatchSize - 1
		if nextToSlot > localLatestSlot {
			nextToSlot = localLatestSlot
		}

		nextReq := SyncRequest{
			RequestID: fmt.Sprintf("auto_sync_%d_%d_%s", nextFromSlot, nextToSlot, targetPeer.String()),
			FromSlot:  nextFromSlot,
			ToSlot:    nextToSlot,
		}

		go func() {
			ln.sendBlocksOverStream(nextReq, targetPeer)
		}()
	}
}

func (ln *Libp2pNetwork) sendSyncRequestToPeer(req SyncRequest, targetPeer peer.ID) error {
	stream, err := ln.host.NewStream(context.Background(), targetPeer, RequestBlockSyncStream)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}
	defer stream.Close()

	data, err := jsonx.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	_, err = stream.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write request: %w", err)
	}

	return nil
}

func (ln *Libp2pNetwork) HandleLatestSlotTopic(ctx context.Context, sub *pubsub.Subscription) {
	logx.Info("NETWORK:LATEST SLOT", "Starting latest slot topic handler")

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
			// skip request from sefl
			if msg.ReceivedFrom == ln.host.ID() {
				continue
			}

			var req LatestSlotRequest
			if err := jsonx.Unmarshal(msg.Data, &req); err != nil {
				logx.Error("NETWORK:LATEST SLOT", "Failed to unmarshal LatestSlotRequest:", err)
				continue
			}

			latestSlot := ln.getLocalLatestSlot()

			ln.sendLatestSlotResponse(msg.ReceivedFrom, latestSlot)
		}
	}
}

func (ln *Libp2pNetwork) getLocalLatestSlot() uint64 {
	return ln.blockStore.GetLatestFinalizedSlot()
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

	data, err := jsonx.Marshal(response)
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
	decoder := jsonx.NewDecoder(s)
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

func (ln *Libp2pNetwork) BroadcastBlock(ctx context.Context, blk *block.BroadcastedBlock) error {
	logx.Info("BLOCK", "Broadcasting block: slot=", blk.Slot)

	data, err := jsonx.Marshal(blk)
	if err != nil {
		logx.Error("BLOCK", "Failed to marshal block: ", err)
		return err
	}

	if ln.topicBlocks != nil {
		logx.Info("BLOCK", "Publishing block to pubsub topic")
		if err := ln.topicBlocks.Publish(ctx, data); err != nil {
			logx.Error("BLOCK", "Failed to publish block:", err)
		}
	}
	return nil
}
