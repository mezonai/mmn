package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/libp2p/go-libp2p/core/peer"
)

// BlockMessageHandler handles incoming block messages
type BlockMessageHandler struct {
	onBlockReceived func(ctx context.Context, from peer.ID, blockData []byte) error
}

// NewBlockMessageHandler creates a new block message handler
func NewBlockMessageHandler(onBlockReceived func(ctx context.Context, from peer.ID, blockData []byte) error) *BlockMessageHandler {
	return &BlockMessageHandler{
		onBlockReceived: onBlockReceived,
	}
}

// HandleMessage implements MessageHandler interface for blocks
func (h *BlockMessageHandler) HandleMessage(ctx context.Context, from peer.ID, data []byte) error {
	log.Printf("Received block message from peer %s", from)

	if h.onBlockReceived != nil {
		return h.onBlockReceived(ctx, from, data)
	}

	return nil
}

// VoteMessageHandler handles incoming vote messages
type VoteMessageHandler struct {
	onVoteReceived func(ctx context.Context, from peer.ID, voteData []byte) error
}

// NewVoteMessageHandler creates a new vote message handler
func NewVoteMessageHandler(onVoteReceived func(ctx context.Context, from peer.ID, voteData []byte) error) *VoteMessageHandler {
	return &VoteMessageHandler{
		onVoteReceived: onVoteReceived,
	}
}

// HandleMessage implements MessageHandler interface for votes
func (h *VoteMessageHandler) HandleMessage(ctx context.Context, from peer.ID, data []byte) error {
	log.Printf("Received vote message from peer %s", from)

	if h.onVoteReceived != nil {
		return h.onVoteReceived(ctx, from, data)
	}

	return nil
}

// TxMessageHandler handles incoming transaction messages
type TxMessageHandler struct {
	onTxReceived func(ctx context.Context, from peer.ID, txData []byte) error
}

// NewTxMessageHandler creates a new transaction message handler
func NewTxMessageHandler(onTxReceived func(ctx context.Context, from peer.ID, txData []byte) error) *TxMessageHandler {
	return &TxMessageHandler{
		onTxReceived: onTxReceived,
	}
}

// HandleMessage implements MessageHandler interface for transactions
func (h *TxMessageHandler) HandleMessage(ctx context.Context, from peer.ID, data []byte) error {
	log.Printf("Received transaction message from peer %s", from)

	if h.onTxReceived != nil {
		return h.onTxReceived(ctx, from, data)
	}

	return nil
}

// PeerDiscoveryMessage represents a peer discovery message
type PeerDiscoveryMessage struct {
	Type      string   `json:"type"`
	PeerID    string   `json:"peer_id"`
	Addresses []string `json:"addresses"`
	Timestamp int64    `json:"timestamp"`
}

// BootstrapHandler handles bootstrap and peer discovery
type BootstrapHandler struct {
	p2pService *P2PService
}

// NewBootstrapHandler creates a new bootstrap handler
func NewBootstrapHandler(p2pService *P2PService) *BootstrapHandler {
	return &BootstrapHandler{
		p2pService: p2pService,
	}
}

// HandlePeerDiscovery handles peer discovery messages
func (h *BootstrapHandler) HandlePeerDiscovery(ctx context.Context, from peer.ID, data []byte) error {
	var msg PeerDiscoveryMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("failed to unmarshal peer discovery message: %w", err)
	}

	log.Printf("Received peer discovery message from %s: type=%s", from, msg.Type)

	switch msg.Type {
	case "PEER_ANNOUNCE":
		// Handle peer announcement
		h.handlePeerAnnounce(ctx, from, &msg)
	case "PEER_REQUEST":
		// Handle peer list request
		h.handlePeerRequest(ctx, from)
	case "PEER_RESPONSE":
		// Handle peer list response
		h.handlePeerResponse(ctx, from, &msg)
	default:
		log.Printf("Unknown peer discovery message type: %s", msg.Type)
	}

	return nil
}

func (h *BootstrapHandler) handlePeerAnnounce(ctx context.Context, from peer.ID, msg *PeerDiscoveryMessage) {
	// Add peer to our list if not already known
	log.Printf("Peer %s announced itself", from)
	// The peer is already connected since we received the message
}

func (h *BootstrapHandler) handlePeerRequest(ctx context.Context, from peer.ID) {
	// Send our known peers to the requesting peer
	peers := h.p2pService.GetConnectedPeers()
	addresses := h.p2pService.GetListenAddresses()

	response := PeerDiscoveryMessage{
		Type:      "PEER_RESPONSE",
		PeerID:    h.p2pService.GetHostID().String(),
		Addresses: addresses,
		Timestamp: getCurrentTimestamp(),
	}

	data, err := json.Marshal(response)
	if err != nil {
		log.Printf("Failed to marshal peer response: %v", err)
		return
	}

	// Send response back to requesting peer
	log.Printf("Sending peer list to %s (%d peers), data size: %d bytes", from, len(peers), len(data))
}

func (h *BootstrapHandler) handlePeerResponse(ctx context.Context, from peer.ID, msg *PeerDiscoveryMessage) {
	// Process received peer list
	log.Printf("Received peer list from %s with %d addresses", from, len(msg.Addresses))

	// Try to connect to new peers
	for _, addrStr := range msg.Addresses {
		// Parse and connect to new peers
		log.Printf("Discovered new peer address: %s", addrStr)
		// Implementation would connect to these peers
	}
}

func getCurrentTimestamp() int64 {
	return int64(0) // Placeholder - would return actual timestamp
}
