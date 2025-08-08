// source: https://github.com/libp2p/go-libp2p/discussions/2926

package main

import (
	"context"
	"encoding/json"
	"mmn/logx"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/routing"
)

func CreateNode(ctx context.Context, cfg *Config) (h host.Host, ddht *dht.IpfsDHT, err error) {
	options := []libp2p.Option{
		libp2p.EnableAutoNATv2(),
		libp2p.NATPortMap(),
		libp2p.EnableNATService(),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			ddht, err = dht.New(ctx, h)
			if err != nil {
				return nil, err
			}
			return ddht, nil
		}),
		libp2p.ForceReachabilityPublic(),
	}

	if cfg.PrivateKey != nil {
		options = append(options, libp2p.Identity(cfg.PrivateKey))
	}

	h, err = libp2p.New(options...)
	if err != nil {
		return nil, ddht, err
	}

	if cfg.Bootstrap {
		if err := ddht.Bootstrap(ctx); err != nil {
			return nil, ddht, err
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
			peerID := c.RemotePeer()
			addrs := c.RemoteMultiaddr()

			logx.Info("BOOTSTRAP NODE", "New peer connected:", addrs.String(), peerID.String())

			go func() {
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
			}()
		},
	})

	logx.Info("BOOTSTRAP NODE", "Node ID:", h.ID())
	for _, addr := range h.Addrs() {
		logx.Info("BOOTSTRAP NODE", "Listening on:", addr)
	}

	return h, ddht, nil
}
