package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/libp2p/go-libp2p/core/peer"

	"mmn/block"
	"mmn/consensus"
)

// NetworkAdapter adapts LibP2P to MMN's networking interfaces
type NetworkAdapter struct {
	p2pService *P2PService
}

// NewNetworkAdapter creates a new network adapter
func NewNetworkAdapter(p2pService *P2PService) *NetworkAdapter {
	return &NetworkAdapter{
		p2pService: p2pService,
	}
}

// BroadcastBlock implements the broadcaster interface for blocks
func (n *NetworkAdapter) BroadcastBlock(ctx context.Context, blk *block.Block) error {
	// Serialize block to protobuf or JSON
	data, err := json.Marshal(blk)
	if err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}

	// Broadcast via pubsub
	if err := n.p2pService.BroadcastBlock(ctx, data); err != nil {
		return fmt.Errorf("failed to broadcast block: %w", err)
	}

	log.Printf("Block %d broadcasted to network", blk.Slot)
	return nil
}

// BroadcastVote implements the broadcaster interface for votes
func (n *NetworkAdapter) BroadcastVote(ctx context.Context, vote *consensus.Vote) error {
	// Serialize vote to JSON
	data, err := json.Marshal(vote)
	if err != nil {
		return fmt.Errorf("failed to marshal vote: %w", err)
	}

	// Broadcast via pubsub
	if err := n.p2pService.BroadcastVote(ctx, data); err != nil {
		return fmt.Errorf("failed to broadcast vote: %w", err)
	}

	log.Printf("Vote for slot %d broadcasted to network", vote.Slot)
	return nil
}

// BroadcastTransaction implements the broadcaster interface for transactions
func (n *NetworkAdapter) BroadcastTransaction(ctx context.Context, txData []byte) error {
	// Broadcast raw transaction data
	if err := n.p2pService.BroadcastTransaction(ctx, txData); err != nil {
		return fmt.Errorf("failed to broadcast transaction: %w", err)
	}

	log.Printf("Transaction broadcasted to network")
	return nil
}

// TxBroadcast implements the legacy broadcaster interface
func (n *NetworkAdapter) TxBroadcast(ctx context.Context, txBytes []byte) error {
	return n.BroadcastTransaction(ctx, txBytes)
} // SendBlockToPeer sends a block to a specific peer
func (n *NetworkAdapter) SendBlockToPeer(ctx context.Context, peerID string, blk *block.Block) error {
	// Convert string to peer.ID
	pid, err := peer.Decode(peerID)
	if err != nil {
		return fmt.Errorf("invalid peer ID %s: %w", peerID, err)
	}

	// Serialize block
	data, err := json.Marshal(blk)
	if err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}

	// Send directly to peer
	return n.p2pService.SendBlockToPeer(ctx, pid, data)
}

// SendVoteToPeer sends a vote to a specific peer
func (n *NetworkAdapter) SendVoteToPeer(ctx context.Context, peerID string, vote *consensus.Vote) error {
	// Convert string to peer.ID
	pid, err := peer.Decode(peerID)
	if err != nil {
		return fmt.Errorf("invalid peer ID %s: %w", peerID, err)
	}

	// Serialize vote
	data, err := json.Marshal(vote)
	if err != nil {
		return fmt.Errorf("failed to marshal vote: %w", err)
	}

	// Send directly to peer
	return n.p2pService.SendVoteToPeer(ctx, pid, data)
}

// GetPeers returns list of connected peers
func (n *NetworkAdapter) GetPeers() []string {
	peers := n.p2pService.GetConnectedPeers()
	peerStrs := make([]string, len(peers))
	for i, pid := range peers {
		peerStrs[i] = pid.String()
	}
	return peerStrs
}

// GetPeerCount returns number of connected peers
func (n *NetworkAdapter) GetPeerCount() int {
	return len(n.p2pService.GetConnectedPeers())
}

// IsConnected checks if we're connected to a specific peer
func (n *NetworkAdapter) IsConnected(peerID string) bool {
	pid, err := peer.Decode(peerID)
	if err != nil {
		return false
	}

	for _, connectedPeer := range n.p2pService.GetConnectedPeers() {
		if connectedPeer == pid {
			return true
		}
	}
	return false
}

// GetNodeID returns this node's peer ID
func (n *NetworkAdapter) GetNodeID() string {
	return n.p2pService.GetHostID().String()
}

// GetListenAddresses returns addresses this node listens on
func (n *NetworkAdapter) GetListenAddresses() []string {
	return n.p2pService.GetListenAddresses()
}
