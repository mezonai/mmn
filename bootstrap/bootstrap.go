package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	connmgr "github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	ma "github.com/multiformats/go-multiaddr"
)

func CreateNode(ctx context.Context, cfg *Config, bootstrapP2pPort string) (h host.Host, ddht *dht.IpfsDHT, err error) {
	lowWater := 80
	highWater := 100
	gracePeriod := time.Minute

	mgr, err := connmgr.NewConnManager(lowWater, highWater, connmgr.WithGracePeriod(gracePeriod))
	if err != nil {
		return nil, nil, err
	}
	bootstrapP2pAddr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%s", bootstrapP2pPort)

	options := []libp2p.Option{
		libp2p.ListenAddrStrings(
			bootstrapP2pAddr,
		),
		libp2p.EnableRelayService(),
		libp2p.ForceReachabilityPublic(),
		libp2p.ConnectionManager(mgr),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			ddht, err = dht.New(ctx, h, dht.Mode(dht.ModeServer))
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

	if err := ddht.Bootstrap(ctx); err != nil {
		return nil, nil, err
	}

	// Start circuit relay service (server side) to relay connections between isolated nodes
	if _, err := relay.New(h); err != nil {
		return nil, nil, fmt.Errorf("failed to enable relay service: %w", err)
	}

	h.SetStreamHandler(NodeInfoProtocol, func(s network.Stream) {
		defer s.Close()

		logx.Debug("NodeInfoProtocol", "new peer stream", h.ID().String())

		info := map[string]interface{}{
			"new_peer_id": h.ID().String(),
			"addrs":       addrStrings(h.Addrs()),
		}

		data, _ := jsonx.Marshal(info)
		s.Write(data)
	})

	// Track which bootstrap local addr a peer dialed so we can craft relay addrs
	peerInboundOn := make(map[peer.ID]ma.Multiaddr)

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
			// Local addr the peer connected to (bootstrap side)
			local := c.LocalMultiaddr()
			peerInboundOn[peerID] = local

			logx.Info("BOOTSTRAP NODE", "New peer connected:", addrs.String(), peerID.String())

			// Delay introductions briefly to allow peers to complete relay reservations
			go func(newPeer peer.ID) {
				time.Sleep(2 * time.Second)
				// Introduce the new peer to all existing peers, and vice versa
				peers := h.Network().Peers()
				for _, p := range peers {
					if p == newPeer {
						continue
					}
					// Send new peer info to existing peer
					go sendPeerInfo(h, p, newPeer, peerInboundOn[p])
					// Send existing peer info to new peer
					go sendPeerInfo(h, newPeer, p, peerInboundOn[newPeer])
				}
				// Also send bootstrap's own info to the new peer (existing behavior)
				go openSteam(newPeer, h)
			}(peerID)
		},

		DisconnectedF: func(n network.Network, c network.Conn) {
			atomic.AddInt32(&ConnCount, -1)
			logx.Info("BOOTSTRAP NODE", "Peer disconnected:", c.RemotePeer())
		},
	})

	logx.Info("BOOTSTRAP NODE", "Node ID:", h.ID())
	for _, addr := range h.Addrs() {
		logx.Info("BOOTSTRAP NODE", "Listening on:", addr)
	}

	for _, addr := range h.Addrs() {
		if strings.HasPrefix(addr.String(), "/ip4") && strings.Contains(addr.String(), "/tcp/") {
			fullAddr := fmt.Sprintf("%s/p2p/%s", addr.String(), h.ID().String())
			logx.Info("BOOTSTRAP NODE", "fullAddr: ", fullAddr)
		}
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
	data, _ := jsonx.Marshal(info)
	stream.Write(data)
}

// sendPeerInfo opens a stream to targetPeer and sends the given peer's ID and addresses.
func sendPeerInfo(h host.Host, targetPeer peer.ID, peerToAnnounce peer.ID, localBootstrapAddr ma.Multiaddr) {
	stream, err := h.NewStream(context.Background(), targetPeer, NodeInfoProtocol)
	if err != nil {
		logx.Error("BOOTSTRAP NODE", "Failed to open stream for peer introduction:", err)
		return
	}
	defer stream.Close()

	// Base addresses known for the announced peer (may not be reachable across networks)
	baseAddrs := h.Peerstore().Addrs(peerToAnnounce)

	// Construct full relay addresses for the announced peer via the bootstrap addr
	// that the target peer is known to reach (localBootstrapAddr). Fallback to all
	// bootstrap addrs if not known.
	var relayAddrs []string
	if localBootstrapAddr != nil {
		relayAddrs = append(relayAddrs, localBootstrapAddr.String()+"/p2p/"+h.ID().String()+"/p2p-circuit/p2p/"+peerToAnnounce.String())
	} else {
		for _, raddr := range h.Addrs() {
			relayStr := raddr.String() + "/p2p/" + h.ID().String() + "/p2p-circuit/p2p/" + peerToAnnounce.String()
			relayAddrs = append(relayAddrs, relayStr)
		}
	}

	// Combine original and relay addrs (relay first to prefer it)
	combined := append(relayAddrs, addrStrings(baseAddrs)...)

	info := map[string]interface{}{
		"new_peer_id": peerToAnnounce.String(),
		"addrs":       combined,
	}
	data, _ := jsonx.Marshal(info)
	if _, err := stream.Write(data); err != nil {
		logx.Error("BOOTSTRAP NODE", "Failed to write peer introduction:", err)
		return
	}
	logx.Info("BOOTSTRAP NODE", "Introduced peer", peerToAnnounce.String(), "to", targetPeer.String())
}
