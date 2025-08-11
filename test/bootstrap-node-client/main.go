package main

import (
	"context"
	"fmt"
	"log"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	peerstore "github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

func main() {
	ctx := context.Background()

	host, err := libp2p.New()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("🚀 Common Peer started")
	fmt.Println("🆔 Peer ID:", host.ID())

	// Address of bootstrap node
	bootstrapAddrStr := "/ip4/10.10.30.50/tcp/59828/p2p/12D3KooWEyDXDXzhpct8kkdV2hvMbLkhagGci5oA9rJwiniXErwn"
	bootstrapAddr, err := ma.NewMultiaddr(bootstrapAddrStr)
	if err != nil {
		log.Fatal(err)
	}

	peerInfo, err := peerstore.AddrInfoFromP2pAddr(bootstrapAddr)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("🔌 Connecting to bootstrap peer:", peerInfo.ID)
	if err := host.Connect(ctx, *peerInfo); err != nil {
		log.Fatalf("❌ Failed to connect: %v", err)
	}

	fmt.Println("✅ Connected to bootstrap peer:", peerInfo.ID, peerInfo.Addrs)

	select {
	case <-ctx.Done():
	case <-time.After(time.Minute):
	}
}
