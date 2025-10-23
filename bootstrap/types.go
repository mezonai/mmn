package bootstrap

import (
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

type PeerInfo struct {
	ID       peer.ID
	Addrs    []ma.Multiaddr
	LastSeen time.Time
	IsActive bool
}

type BootstrapNode struct {
}

type Config struct {
	PrivateKey crypto.PrivKey
	Bootstrap  bool
}
