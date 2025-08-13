package bootstrap

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
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
	host  host.Host
	peers map[peer.ID]*PeerInfo
	mu    sync.RWMutex
}

type mdnsNotifee struct {
	host host.Host
}

type Config struct {
	PrivateKey crypto.PrivKey
	Bootstrap  bool
}
