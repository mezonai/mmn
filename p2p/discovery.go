package p2p

import (
	"context"
	"encoding/json"
	"mmn/discovery"
	"mmn/logx"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

func (ln *Libp2pNetwork) RequestNodeInfo(bootstrapPeer string, info *peer.AddrInfo) error {
	ctx := context.Background()

	logx.Info("NETWORK CONNECTED AND REQYEST NODE INFO TO JOIN", bootstrapPeer)

	if len(ln.host.Network().Peers()) < int(ln.maxPeers) {
		if err := ln.host.Connect(ctx, *info); err != nil {
			// logx.Error("NETWORK:SETUP", "connect bootstrap", err.Error())
			// ignore logs for handle case node is a boostrap node
		}
	}

	return nil
}

func (ln *Libp2pNetwork) Discovery(discovery discovery.Discovery, ctx context.Context, h host.Host) {
	func() {
		for {
			peerChan, err := discovery.FindPeers(ctx, AdvertiseName, int(ln.maxPeers))
			if err != nil {
				logx.Error("DISCOVERY", "Failed to find peers:", err)
				time.Sleep(10 * time.Second)
				continue
			}

			for p := range peerChan {
				if p.ID == h.ID() || len(p.Addrs) == 0 {
					continue
				}

				if len(h.Network().Peers()) >= int(ln.maxPeers) {
					break
				}

				err := h.Connect(ctx, p)
				if err != nil {
					logx.Error("DISCOVERY", "Failed to connect to discovered peer:", err)
				} else {
					logx.Info("DISCOVERY", "Connected to discovered peer:", p.ID.String())
				}
			}

			time.Sleep(30 * time.Second)
		}
	}()
}

func HandleNewpPeerConnected(h host.Host) {
	h.SetStreamHandler(NodeInfoProtocol, func(s network.Stream) {
		defer s.Close()

		info := map[string]interface{}{
			"new_peer_id": h.ID().String(),
			"addrs":       AddrStrings(h.Addrs()),
		}

		data, _ := json.Marshal(info)
		s.Write(data)
	})
}

func openStream(peerID peer.ID, h host.Host) {
	stream, err := h.NewStream(context.Background(), peerID, NodeInfoProtocol)
	if err != nil {
		logx.Error("BOOTSTRAP NODE", "Failed to open stream to new peer:", err)
		return
	}
	defer stream.Close()

	info := map[string]interface{}{
		"new_peer_id": h.ID().String(),
		"addrs":       AddrStrings(h.Addrs()),
	}
	data, _ := json.Marshal(info)
	stream.Write(data)
}

func NotifyNewPeerConnected(h host.Host) {
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

			go openStream(peerID, h)
		},

		DisconnectedF: func(n network.Network, c network.Conn) {
			atomic.AddInt32(&ConnCount, -1)
			logx.Info("BOOTSTRAP NODE", "Peer disconnected:", c.RemotePeer())
		},
	})
}
