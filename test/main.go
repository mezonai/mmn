package main

import (
	"log"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

// mDNS Notifee
type discoveryNotifee struct{}

func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	log.Printf("✅ Discovered new peer via mDNS: %s", pi.ID.String())
}

func setupMDNS(h host.Host) {
	// Create the mDNS service
	service := mdns.NewMdnsService(h, "my-mdns", &discoveryNotifee{})

	// Start the mDNS service
	if err := service.Start(); err != nil {
		log.Fatalf("❌ Failed to start mDNS service: %v", err)
	}

	log.Println("🔍 mDNS service started")
}

func main() {
	// Create a libp2p host
	h, err := libp2p.New()
	if err != nil {
		log.Fatalf("❌ Failed to create host: %v", err)
	}

	log.Printf("🚀 Node started. ID: %s", h.ID().String())
	log.Printf("🌐 Listening addresses: %v", h.Addrs())

	// Start mDNS
	setupMDNS(h)

	// Keep the main function running
	select {}
}
