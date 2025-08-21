package p2p

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/types"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

// HandleShredTopic handles incoming shred messages
func (ln *Libp2pNetwork) HandleShredTopic(ctx context.Context, sub *pubsub.Subscription) {
	for {
		select {
		case <-ctx.Done():
			logx.Info("NETWORK:SHRED", "Stopping shred topic handler")
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logx.Warn("NETWORK:SHRED", "Next error:", err)
				continue
			}

			if msg.ReceivedFrom == ln.host.ID() {
				logx.Debug("NETWORK:SHRED", "Skipping shred message from self")
				continue
			}

			var shredMsg ShredMessage
			if err := json.Unmarshal(msg.Data, &shredMsg); err != nil {
				logx.Warn("NETWORK:SHRED", "Unmarshal error:", err)
				continue
			}

			// Convert to Shred type
			shred := &types.Shred{
				Header: types.ShredHeader{
					Signature:   [64]byte{},
					Slot:        shredMsg.Slot,
					Index:       shredMsg.Index,
					FECSetIndex: shredMsg.FECSetIndex,
					Version:     shredMsg.Version,
					Type:        types.ShredType(shredMsg.Type),
					Flags:       shredMsg.Flags,
					PayloadSize: shredMsg.PayloadSize,
				},
				Payload: shredMsg.Payload,
			}
			copy(shred.Header.Signature[:], shredMsg.Signature)

			if ln.onShredReceived != nil {
				if err := ln.onShredReceived(shred); err != nil {
					logx.Error("NETWORK:SHRED", "Processing shred error:", err)
				}
			}
		}
	}
}

// HandleRepairRequestTopic handles repair requests
func (ln *Libp2pNetwork) HandleRepairRequestTopic(ctx context.Context, sub *pubsub.Subscription) {
	for {
		select {
		case <-ctx.Done():
			logx.Info("NETWORK:REPAIR", "Stopping repair request topic handler")
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logx.Warn("NETWORK:REPAIR", "Next error:", err)
				continue
			}

			if msg.ReceivedFrom == ln.host.ID() {
				logx.Debug("NETWORK:REPAIR", "Skipping repair request from self")
				continue
			}

			var repairReq RepairRequestMessage
			if err := json.Unmarshal(msg.Data, &repairReq); err != nil {
				logx.Warn("NETWORK:REPAIR", "Unmarshal error:", err)
				continue
			}

			logx.Info("NETWORK:REPAIR", fmt.Sprintf("Got repair request for slot %d, FEC set %d",
				repairReq.Slot, repairReq.FECSetIndex))

			// Handle repair request - this would need to be implemented
			// based on how you store shreds locally
			// For now, just log the request
		}
	}
}

// HandleRepairResponseTopic handles repair responses
func (ln *Libp2pNetwork) HandleRepairResponseTopic(ctx context.Context, sub *pubsub.Subscription) {
	for {
		select {
		case <-ctx.Done():
			logx.Info("NETWORK:REPAIR", "Stopping repair response topic handler")
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logx.Warn("NETWORK:REPAIR", "Next error:", err)
				continue
			}

			if msg.ReceivedFrom == ln.host.ID() {
				logx.Debug("NETWORK:REPAIR", "Skipping repair response from self")
				continue
			}

			var repairResp RepairResponseMessage
			if err := json.Unmarshal(msg.Data, &repairResp); err != nil {
				logx.Warn("NETWORK:REPAIR", "Unmarshal error:", err)
				continue
			}

			logx.Info("NETWORK:REPAIR", fmt.Sprintf("Got repair response for slot %d with %d shreds",
				repairResp.Slot, len(repairResp.Shreds)))

			// Process each repaired shred
			for _, shredMsg := range repairResp.Shreds {
				shred := &types.Shred{
					Header: types.ShredHeader{
						Signature:   [64]byte{},
						Slot:        shredMsg.Slot,
						Index:       shredMsg.Index,
						FECSetIndex: shredMsg.FECSetIndex,
						Version:     shredMsg.Version,
						Type:        types.ShredType(shredMsg.Type),
						Flags:       shredMsg.Flags,
						PayloadSize: shredMsg.PayloadSize,
					},
					Payload: shredMsg.Payload,
				}
				copy(shred.Header.Signature[:], shredMsg.Signature)

				if ln.onShredReceived != nil {
					if err := ln.onShredReceived(shred); err != nil {
						logx.Error("NETWORK:REPAIR", "Processing repaired shred error:", err)
					}
				}
			}
		}
	}
}

// BroadcastShred broadcasts a single shred
func (ln *Libp2pNetwork) BroadcastShred(ctx context.Context, shred *types.Shred) error {
	shredMsg := ShredMessage{
		Slot:        shred.Header.Slot,
		Index:       shred.Header.Index,
		FECSetIndex: shred.Header.FECSetIndex,
		Version:     shred.Header.Version,
		Type:        uint8(shred.Header.Type),
		Flags:       shred.Header.Flags,
		PayloadSize: shred.Header.PayloadSize,
		Signature:   shred.Header.Signature[:],
		Payload:     shred.Payload,
	}

	data, err := json.Marshal(shredMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal shred: %w", err)
	}

	if ln.topicShreds != nil {
		if err := ln.topicShreds.Publish(ctx, data); err != nil {
			return fmt.Errorf("failed to publish shred: %w", err)
		}
	}

	return nil
}

// BroadcastShreds broadcasts multiple shreds efficiently
func (ln *Libp2pNetwork) BroadcastShreds(ctx context.Context, shreds []types.Shred) error {
	for i := range shreds {
		if err := ln.BroadcastShred(ctx, &shreds[i]); err != nil {
			logx.Error("NETWORK:SHRED", fmt.Sprintf("Failed to broadcast shred %d: %v", i, err))
			// Continue broadcasting other shreds even if one fails
		}
	}
	return nil
}

// RequestRepair requests missing shreds for a specific FEC set
func (ln *Libp2pNetwork) RequestRepair(ctx context.Context, slot uint64, fecSetIndex uint32, missing []uint32) error {
	repairReq := RepairRequestMessage{
		Slot:        slot,
		FECSetIndex: fecSetIndex,
		Missing:     missing,
	}

	data, err := json.Marshal(repairReq)
	if err != nil {
		return fmt.Errorf("failed to marshal repair request: %w", err)
	}

	if ln.topicRepairReq != nil {
		if err := ln.topicRepairReq.Publish(ctx, data); err != nil {
			return fmt.Errorf("failed to publish repair request: %w", err)
		}
	}

	logx.Info("NETWORK:REPAIR", fmt.Sprintf("Requested repair for slot %d, FEC set %d, missing %v",
		slot, fecSetIndex, missing))

	return nil
}
