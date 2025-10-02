package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/logx"
)

func (ln *Libp2pNetwork) handleRepairBlockStream(s network.Stream) {
	defer s.Close()

	var req RepairRequest
	decoder := json.NewDecoder(s)
	if err := decoder.Decode(&req); err != nil {
		logx.Error("NETWORK:REPAIR BLOCK", "Failed to decode RepairRequest:", err)
		return
	}

	blk := ln.memBlockStore.GetBlock(req.Slot, req.BlockHash)
	if blk == nil {
		logx.Error("NETWORK:REPAIR BLOCK", "Block not found for slot:", req.Slot)
		return
	}

	encoder := json.NewEncoder(s)
	if err := encoder.Encode(blk); err != nil {
		logx.Error("NETWORK:REPAIR BLOCK", "Failed to send block:", err)
		return
	}
}

func (ln *Libp2pNetwork) RequestRepairBlock(ctx context.Context, slot uint64, blockHash [32]byte) (*block.BroadcastedBlock, error) {
	selfID := ln.host.ID()
	peers := ln.host.Network().Peers()
	for _, peerID := range peers {
		if peerID == selfID {
			continue
		}

		// Set timeout for each peer request
		peerCtx, cancel := context.WithTimeout(ctx, 3*time.Second) //TODO: Adjust timeout as needed
		defer cancel()

		stream, err := ln.host.NewStream(peerCtx, peerID, RepairBlockProtocol)
		if err != nil {
			continue
		}

		// Send RepairRequest
		req := RepairRequest{Slot: slot, BlockHash: blockHash}
		encoder := json.NewEncoder(stream)
		if err := encoder.Encode(req); err != nil {
			stream.Close()
			continue
		}

		// Set deadline for receiving block
		stream.SetReadDeadline(time.Now().Add(3 * time.Second)) //TODO: Adjust timeout as needed

		var blk block.BroadcastedBlock
		decoder := json.NewDecoder(stream)
		err = decoder.Decode(&blk)
		stream.Close()
		if err == nil {
			return &blk, nil
		}
	}
	return nil, fmt.Errorf("failed to get block from any peer")
}
