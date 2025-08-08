package main

import (
	"context"
	"fmt"
	"log"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

func main() {
	ctx := context.Background()

	// Bootstrap listening address
	listenAddr, _ := ma.NewMultiaddr("/ip4/127.0.0.1/tcp/9001")

	// Create the bootstrap node
	host, err := libp2p.New(libp2p.ListenAddrs(listenAddr))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("✅ Bootstrap node is running")
	fmt.Println("🆔 Peer ID:", host.ID())
	for _, addr := range host.Addrs() {
		fmt.Printf("📡 Listening on: %s/p2p/%s\n", addr, host.ID())
	}

	// 👂 Handle new connections
	host.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(n network.Network, c network.Conn) {
			fmt.Println("🔗 New peer connected:")
			printPeerInfo(c.RemotePeer(), c)
		},
	})

	// Keep running
	select {
	case <-ctx.Done():
	case <-time.After(time.Hour):
	}
}

// Print connected peer info
func printPeerInfo(pid peer.ID, conn network.Conn) {
	fmt.Println("👉 Peer Info:")
	fmt.Println(" - Peer ID:   ", pid.String())
	fmt.Println(" - Remote Addr:", conn.RemoteMultiaddr())
	fmt.Println(" - Direction: ", conn.Stat().Direction.String())
}
