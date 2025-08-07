package p2p

import (
	"context"
	"fmt"
	"log"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// BroadcastBlock broadcasts a block to all peers via pubsub
func (p *P2PService) BroadcastBlock(ctx context.Context, data []byte) error {
	if p.pubsub == nil {
		return fmt.Errorf("pubsub not enabled")
	}

	if p.blockTopic == nil {
		return fmt.Errorf("block topic not joined")
	}

	if err := p.blockTopic.Publish(ctx, data); err != nil {
		return fmt.Errorf("failed to publish block: %w", err)
	}

	log.Printf("Broadcasted block to network")
	return nil
}

// BroadcastVote broadcasts a vote to all peers via pubsub
func (p *P2PService) BroadcastVote(ctx context.Context, data []byte) error {
	if p.pubsub == nil {
		return fmt.Errorf("pubsub not enabled")
	}

	if p.voteTopic == nil {
		return fmt.Errorf("vote topic not joined")
	}

	if err := p.voteTopic.Publish(ctx, data); err != nil {
		return fmt.Errorf("failed to publish vote: %w", err)
	}

	log.Printf("Broadcasted vote to network")
	return nil
}

// BroadcastTransaction broadcasts a transaction to all peers via pubsub
func (p *P2PService) BroadcastTransaction(ctx context.Context, data []byte) error {
	if p.pubsub == nil {
		return fmt.Errorf("pubsub not enabled")
	}

	if p.txTopic == nil {
		return fmt.Errorf("tx topic not joined")
	}

	if err := p.txTopic.Publish(ctx, data); err != nil {
		return fmt.Errorf("failed to publish transaction: %w", err)
	}

	log.Printf("Broadcasted transaction to network")
	return nil
}

// SendBlockToPeer sends a block directly to a specific peer
func (p *P2PService) SendBlockToPeer(ctx context.Context, peerID peer.ID, data []byte) error {
	stream, err := p.host.NewStream(ctx, peerID, protocol.ID(BlockProtocol))
	if err != nil {
		return fmt.Errorf("failed to create stream to peer %s: %w", peerID, err)
	}
	defer stream.Close()

	if _, err := stream.Write(data); err != nil {
		return fmt.Errorf("failed to write block to stream: %w", err)
	}

	log.Printf("Sent block to peer %s", peerID)
	return nil
}

// SendVoteToPeer sends a vote directly to a specific peer
func (p *P2PService) SendVoteToPeer(ctx context.Context, peerID peer.ID, data []byte) error {
	stream, err := p.host.NewStream(ctx, peerID, protocol.ID(VoteProtocol))
	if err != nil {
		return fmt.Errorf("failed to create stream to peer %s: %w", peerID, err)
	}
	defer stream.Close()

	if _, err := stream.Write(data); err != nil {
		return fmt.Errorf("failed to write vote to stream: %w", err)
	}

	log.Printf("Sent vote to peer %s", peerID)
	return nil
}

// SendTransactionToPeer sends a transaction directly to a specific peer
func (p *P2PService) SendTransactionToPeer(ctx context.Context, peerID peer.ID, data []byte) error {
	stream, err := p.host.NewStream(ctx, peerID, protocol.ID(TxProtocol))
	if err != nil {
		return fmt.Errorf("failed to create stream to peer %s: %w", peerID, err)
	}
	defer stream.Close()

	if _, err := stream.Write(data); err != nil {
		return fmt.Errorf("failed to write transaction to stream: %w", err)
	}

	log.Printf("Sent transaction to peer %s", peerID)
	return nil
}

// GetConnectedPeers returns a list of currently connected peers
func (p *P2PService) GetConnectedPeers() []peer.ID {
	var connected []peer.ID
	for _, conn := range p.host.Network().Conns() {
		if conn.Stat().Direction == network.DirOutbound || conn.Stat().Direction == network.DirInbound {
			connected = append(connected, conn.RemotePeer())
		}
	}
	return connected
}

// GetHostID returns this node's peer ID
func (p *P2PService) GetHostID() peer.ID {
	return p.host.ID()
}

// GetListenAddresses returns the addresses this node is listening on
func (p *P2PService) GetListenAddresses() []string {
	addrs := p.host.Addrs()
	addrStrs := make([]string, len(addrs))
	for i, addr := range addrs {
		addrStrs[i] = addr.String()
	}
	return addrStrs
}
