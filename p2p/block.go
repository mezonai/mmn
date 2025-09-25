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
	"github.com/mezonai/mmn/poh"
	"github.com/mezonai/mmn/transaction"

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

			// Check if this request is already being handled with proper locking
			ln.syncTrackerMu.Lock()
			tracker, exists := ln.syncRequests[req.RequestID]
			if !exists {
				// Create new tracker
				tracker = NewSyncRequestTracker(req.RequestID, req.FromSlot, req.ToSlot)
				ln.syncRequests[req.RequestID] = tracker
				logx.Info("NETWORK:SYNC BLOCK", "Created new tracker for request:", req.RequestID)
			}
			ln.syncTrackerMu.Unlock()

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
	ln.syncTrackerMu.Unlock()

	if !tracker.ActivatePeer(remotePeer, s) {
		return
	}

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

		logx.Info("NETWORK:SYNC BLOCK", "Received batch of", len(blocks), "blocks, from slot", blocks[0].Slot, "to slot", blocks[len(blocks)-1].Slot)

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

func (ln *Libp2pNetwork) convertBlockToBroadcastedBlock(blk *block.Block) *block.BroadcastedBlock {
	if blk == nil {
		return nil
	}

	txStore := ln.blockStore.GetTxStore()
	if txStore == nil {
		return nil
	}

	entries := make([]poh.Entry, len(blk.Entries))
	for i, persistentEntry := range blk.Entries {
		transactions, err := txStore.GetBatch(persistentEntry.TxHashes)
		if err != nil {
			logx.Warn("NETWORK:SYNC BLOCK", fmt.Sprintf("Failed to load transactions for entry %d in block %d: %v", i, blk.Slot, err))
			transactions = make([]*transaction.Transaction, 0)
		}

		entries[i] = poh.Entry{
			NumHashes:    persistentEntry.NumHashes,
			Hash:         persistentEntry.Hash,
			Transactions: transactions,
			Tick:         persistentEntry.Tick,
		}
	}

	broadcastedBlock := &block.BroadcastedBlock{
		BlockCore: blk.BlockCore,
		Entries:   entries,
	}

	return broadcastedBlock
}

func (ln *Libp2pNetwork) sendBlockBatchStream(batch []*block.BroadcastedBlock, s network.Stream) error {
	if len(batch) == 0 {
		return nil
	}
	logx.Info("NETWORK:SYNC BLOCK", "Sending batch of", len(batch), "blocks, from slot", batch[0].Slot, "to slot", batch[len(batch)-1].Slot)

	data, err := jsonx.Marshal(batch)
	if err != nil {
		return fmt.Errorf("failed to marshal batch: %w", err)
	}

	if err := s.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil {
		logx.Warn("NETWORK:SYNC BLOCK", "Failed to set write deadline:", err)
	}

	bytesWritten, err := s.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write batch to stream: %w", err)
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	stream, err := ln.host.NewStream(ctx, targetPeer, RequestBlockSyncStream)
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

	// Track original request ID for cleanup
	originalRequestID := req.RequestID
	defer func() {
		ln.syncTrackerMu.Lock()
		if tracker, exists := ln.syncRequests[originalRequestID]; exists {
			tracker.CloseRequest()
			delete(ln.syncRequests, originalRequestID)
		}
		ln.syncTrackerMu.Unlock()
	}()

	localLatestSlot := ln.blockStore.GetLatestFinalizedSlot()
	if localLatestSlot > 0 && req.FromSlot > localLatestSlot {
		return
	}

	var batch []*block.BroadcastedBlock
	totalBlocksSent := 0
	currentFromSlot := req.FromSlot
	currentToSlot := req.ToSlot

	// Use iterative approach instead of recursion to prevent goroutine leaks
	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			logx.Warn("NETWORK:SYNC BLOCK", "Context cancelled, stopping sync for peer:", targetPeer.String())
			return
		default:
		}

		// Refresh latest slot for each iteration
		localLatestSlot = ln.blockStore.GetLatestFinalizedSlot()

		// Safety check to prevent infinite loop
		if currentFromSlot > localLatestSlot {
			break
		}

		// Adjust currentToSlot if it exceeds local latest slot
		if currentToSlot > localLatestSlot {
			currentToSlot = localLatestSlot
		}

		slot := currentFromSlot
		for slot <= currentToSlot {
			blk := ln.blockStore.Block(slot)
			if blk != nil {
				broadcastedBlock := ln.convertBlockToBroadcastedBlock(blk)
				batch = append(batch, broadcastedBlock)
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
				logx.Error("NETWORK:SYNC BLOCK", "Failed to send final batch:", err.Error())
				return
			}
			totalBlocksSent += len(batch)
			batch = batch[:0]
		}

		// Jump to next batch
		currentFromSlot = currentToSlot + 1
		currentToSlot = currentFromSlot + SyncBlocksBatchSize - 1
	}

	logx.Info("NETWORK:SYNC BLOCK", "Completed sync for peer:", targetPeer.String(), "total blocks sent:", totalBlocksSent)
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
			latestPohSlot := ln.OnGetLatestPohSlot()

			ln.sendLatestSlotResponse(msg.ReceivedFrom, latestSlot, latestPohSlot)
		}
	}
}

func (ln *Libp2pNetwork) getLocalLatestSlot() uint64 {
	return ln.blockStore.GetLatestFinalizedSlot()
}

func (ln *Libp2pNetwork) sendLatestSlotResponse(targetPeer peer.ID, latestSlot uint64, latestPohSlot uint64) {
	stream, err := ln.host.NewStream(context.Background(), targetPeer, LatestSlotProtocol)
	if err != nil {
		logx.Error("NETWORK:LATEST SLOT", "Failed to open stream to peer:", err)
		return
	}
	defer stream.Close()

	response := LatestSlotResponse{
		LatestSlot:    latestSlot,
		LatestPohSlot: latestPohSlot,
		PeerID:        ln.host.ID().String(),
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
		if err := ln.onLatestSlotReceived(response.LatestSlot, response.LatestPohSlot, response.PeerID); err != nil {
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
