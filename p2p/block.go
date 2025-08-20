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
				logx.Info("NETWORK:BLOCK", "Received block from peer:", msg.ReceivedFrom.String(), "slot:", blk.Slot)

				// Process block and update peer score based on validity
				err := ln.onBlockReceived(blk)
				if err != nil {
					// Invalid block - penalize peer
					ln.UpdatePeerScore(msg.ReceivedFrom, "invalid_block", nil)
					logx.Warn("NETWORK:BLOCK", "Invalid block from peer:", msg.ReceivedFrom.String(), "error:", err)
				} else {
					// Valid block - reward peer
					ln.UpdatePeerScore(msg.ReceivedFrom, "valid_block", nil)
				}
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
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				logx.Error("NETWORK:SYNC BLOCK", "Failed to unmarshal SyncRequest: ", err.Error())
				continue
			}

			// Skip requests from self
			if msg.ReceivedFrom == ln.host.ID() {
				continue
			}

			logx.Info("NETWORK:SYNC BLOCK", "Received sync request: ", req.RequestID, " from slot ", req.FromSlot, " to slot ", req.ToSlot, " from peer: ", msg.ReceivedFrom.String())

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
			}

			if !tracker.ActivatePeer(msg.ReceivedFrom, nil) {
				continue
			}

			// Send blocks in a goroutine to avoid blocking
			go func(request SyncRequest, peer peer.ID) {
				ln.sendBlocksOverStream(request, peer)
			}(req, msg.ReceivedFrom)
		}
	}
}

func (ln *Libp2pNetwork) handleBlockSyncRequestStream(s network.Stream) {
	defer s.Close()

	remotePeer := s.Conn().RemotePeer()

	var syncRequest SyncRequest
	decoder := json.NewDecoder(s)
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

	for {
		var blocks []*block.Block
		if err := decoder.Decode(&blocks); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			logx.Error("NETWORK:SYNC BLOCK", "Failed to decode blocks array: ", err.Error())
			break
		}

		batchCount++
		totalBlocks += len(blocks)

		hasDuplicate := false
		for _, blk := range blocks {
			if blk != nil {
				if existingBlock := ln.blockStore.Block(blk.Slot); existingBlock != nil {
					hasDuplicate = true
					break
				}
			}
		}

		// stops stream if dulicated
		if hasDuplicate {
			logx.Info("NETWORK:SYNC BLOCK", "Closing stream due to duplicate blocks")
			break
		}

		if ln.onSyncResponseReceived != nil {
			if err := ln.onSyncResponseReceived(blocks); err != nil {
				logx.Error("NETWORK:SYNC BLOCK", "Failed to process sync response: ", err.Error())
			} else {
				logx.Info("NETWORK:SYNC BLOCK", "Sync response callback completed successfully for batch ", batchCount)
				tracker.CloseAllOtherPeers()
				break
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

	// Close all peer streams and remove tracker
	ln.syncTrackerMu.Lock()
	if tracker, exists := ln.syncRequests[syncRequest.RequestID]; exists {
		tracker.CloseAllPeers()
		delete(ln.syncRequests, syncRequest.RequestID)
	}
	ln.syncTrackerMu.Unlock()
}

func (ln *Libp2pNetwork) sendBlockBatchStream(batch []*block.Block, s network.Stream) error {
	data, err := json.Marshal(batch)
	if err != nil {
		return err
	}

	_, err = s.Write(data)
	if err != nil {
		return err
	}

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

	return nil
}

func (ln *Libp2pNetwork) sendBlocksOverStream(req SyncRequest, targetPeer peer.ID) {

	stream, err := ln.host.NewStream(context.Background(), targetPeer, RequestBlockSyncStream)
	if err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "Failed to create stream to peer:", targetPeer.String(), "error:", err.Error())
		return
	}
	defer stream.Close()

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(req); err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "Failed to send request ID:", err)
		return
	}

	// Cclose when done
	defer func() {
		ln.syncTrackerMu.Lock()
		if tracker, exists := ln.syncRequests[req.RequestID]; exists {
			tracker.CloseRequest()
			delete(ln.syncRequests, req.RequestID)
		}
		ln.syncTrackerMu.Unlock()
	}()

	localLatestSlot := ln.blockStore.GetLatestSlot()
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

		if len(batch) >= int(BatchSize) {
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

	if req.ToSlot < localLatestSlot {
		nextFromSlot := req.ToSlot + 1
		nextToSlot := nextFromSlot + BatchSize - 1
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

func (ln *Libp2pNetwork) RequestContinuousBlockSync(fromSlot, toSlot uint64, targetPeer peer.ID) {

	currentSlot := fromSlot

	for currentSlot < toSlot {
		endSlot := currentSlot + BatchSize - 1
		if endSlot > toSlot {
			endSlot = toSlot
		}

		requestID := fmt.Sprintf("sync_%d_%d_%s", currentSlot, endSlot, targetPeer.String())

		req := SyncRequest{
			RequestID: requestID,
			FromSlot:  currentSlot,
			ToSlot:    endSlot,
		}

		logx.Info("NETWORK:SYNC BLOCK", "Sending sync request:", requestID, "for slots", currentSlot, "to", endSlot)

		if err := ln.sendSyncRequestToPeer(req, targetPeer); err != nil {
			logx.Error("NETWORK:SYNC BLOCK", "Failed to send sync request:", err)
			continue
		}

		currentSlot = endSlot + 1
	}

	logx.Info("NETWORK:SYNC BLOCK", "Completed sending all sync requests from slot", fromSlot, "to", toSlot)
}

func (ln *Libp2pNetwork) sendSyncRequestToPeer(req SyncRequest, targetPeer peer.ID) error {
	stream, err := ln.host.NewStream(context.Background(), targetPeer, RequestBlockSyncStream)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}
	defer stream.Close()

	data, err := json.Marshal(req)
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
