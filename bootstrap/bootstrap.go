package bootstrap

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/mezonai/mmn/logx"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	connmgr "github.com/libp2p/go-libp2p/p2p/net/connmgr"
)

func CreateNode(ctx context.Context, cfg *Config) (h host.Host, ddht *dht.IpfsDHT, err error) {
	// Create a connection manager
	lowWater := 80
	highWater := 100
	gracePeriod := time.Minute

	mgr, err := connmgr.NewConnManager(lowWater, highWater, connmgr.WithGracePeriod(gracePeriod))
	if err != nil {
		return nil, nil, err
	}

	options := []libp2p.Option{
		libp2p.EnableAutoNATv2(),
		libp2p.NATPortMap(),
		libp2p.EnableNATService(),
		libp2p.ForceReachabilityPublic(),
		libp2p.ConnectionManager(mgr),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			ddht, err = dht.New(ctx, h, dht.Mode(dht.ModeServer)) // server mode for stable DHT nodes
			if err != nil {
				return nil, err
			}
			return ddht, nil
		}),
	}

	if cfg.PrivateKey != nil {
		options = append(options, libp2p.Identity(cfg.PrivateKey))
	}

	h, err = libp2p.New(options...)
	if err != nil {
		return nil, nil, err
	}

	if cfg.Bootstrap {
		if err := ddht.Bootstrap(ctx); err != nil {
			return nil, nil, err
		}
	}

	h.SetStreamHandler(NodeInfoProtocol, func(s network.Stream) {
		defer s.Close()

		info := map[string]interface{}{
			"new_peer_id": h.ID().String(),
			"addrs":       addrStrings(h.Addrs()),
		}

		data, _ := json.Marshal(info)
		s.Write(data)
	})

	h.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(n network.Network, c network.Conn) {
			current := atomic.AddInt32(&ConnCount, 1)
			if current > MaxPeers {
				logx.Info("BOOTSTRAP NODE", "Max peer limit reached. Closing connection to:", c.RemotePeer())
				_ = c.Close()
				atomic.AddInt32(&ConnCount, -1)
				return
			}

			peerID := c.RemotePeer()
			addrs := c.RemoteMultiaddr()

			logx.Info("BOOTSTRAP NODE", "New peer connected:", addrs.String(), peerID.String())

			go openSteam(peerID, h)

		},

		DisconnectedF: func(n network.Network, c network.Conn) {
			atomic.AddInt32(&ConnCount, -1)
			logx.Info("BOOTSTRAP NODE", "Peer disconnected:", c.RemotePeer())
		},
	})

	logx.Info("BOOTSTRAP NODE", "Node ID: ", h.ID())
	for _, addr := range h.Addrs() {
		logx.Info("BOOTSTRAP NODE", "Listening on:", addr)
	}

	return h, ddht, nil
}

func openSteam(peerID peer.ID, h host.Host) {
	stream, err := h.NewStream(context.Background(), peerID, NodeInfoProtocol)
	if err != nil {
		logx.Error("BOOTSTRAP NODE", "Failed to open stream to new peer:", err)
		return
	}
	defer stream.Close()

	info := map[string]interface{}{
		"new_peer_id": h.ID().String(),
		"addrs":       addrStrings(h.Addrs()),
	}
	data, _ := json.Marshal(info)
	stream.Write(data)

}
