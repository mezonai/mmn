package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mmn/p2p"

	"github.com/libp2p/go-libp2p/core/peer"
)

func main() {
	log.Println("Starting MMN P2P integration example...")

	// Create P2P service configuration
	cfg := &p2p.Config{
		ListenAddrs:    []string{"/ip4/0.0.0.0/tcp/4000"},
		BootstrapPeers: []string{}, // No bootstrap for this example
		EnableDHT:      true,
		EnablePubSub:   true,
	}

	// Create P2P service
	p2pService, err := p2p.NewP2PService(cfg)
	if err != nil {
		log.Fatalf("Failed to create P2P service: %v", err)
	}
	defer p2pService.Close()

	// Set up message handlers
	blockHandler := p2p.NewBlockMessageHandler(handleBlockMessage)
	voteHandler := p2p.NewVoteMessageHandler(handleVoteMessage)
	txHandler := p2p.NewTxMessageHandler(handleTxMessage)

	p2pService.SetMessageHandlers(blockHandler, voteHandler, txHandler)

	// Create network adapter
	networkAdapter := p2p.NewNetworkAdapter(p2pService)

	// Start P2P service
	if err := p2pService.Start(); err != nil {
		log.Fatalf("Failed to start P2P service: %v", err)
	}

	log.Printf("P2P Node started successfully!")
	log.Printf("Node ID: %s", networkAdapter.GetNodeID())
	log.Printf("Listen addresses: %v", networkAdapter.GetListenAddresses())

	// Simulate some network activity
	go simulateNetworkActivity(networkAdapter)

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	log.Println("Shutting down P2P node...")
}

func handleBlockMessage(ctx context.Context, from peer.ID, blockData []byte) error {
	log.Printf("Received block from %s, size: %d bytes", from, len(blockData))
	// Process block data here
	return nil
}

func handleVoteMessage(ctx context.Context, from peer.ID, voteData []byte) error {
	log.Printf("Received vote from %s, size: %d bytes", from, len(voteData))
	// Process vote data here
	return nil
}

func handleTxMessage(ctx context.Context, from peer.ID, txData []byte) error {
	log.Printf("Received transaction from %s, size: %d bytes", from, len(txData))
	// Process transaction data here
	return nil
}

func simulateNetworkActivity(adapter *p2p.NetworkAdapter) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Printf("Network status:")
			log.Printf("  - Connected peers: %d", adapter.GetPeerCount())
			log.Printf("  - Node ID: %s", adapter.GetNodeID())

			// Simulate broadcasting a test message
			ctx := context.Background()
			testData := []byte("test transaction data")
			if err := adapter.BroadcastTransaction(ctx, testData); err != nil {
				log.Printf("Failed to broadcast test transaction: %v", err)
			} else {
				log.Printf("Broadcasted test transaction")
			}
		}
	}
}
