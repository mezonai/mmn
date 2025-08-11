package p2p

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"mmn/blockstore"
	"mmn/discovery"
	"mmn/logx"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	ma "github.com/multiformats/go-multiaddr"
)

func NewNetWork(
	selfPubKey string,
	selfPrivKey ed25519.PrivateKey,
	listenAddr string,
	bootstrapPeer string,
	blockStore *blockstore.BlockStore,
) (*Libp2pNetwork, error) {

	privKey, err := crypto.UnmarshalEd25519PrivateKey(selfPrivKey)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal ed25519 private key: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	var ddht *dht.IpfsDHT

	h, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings(listenAddr),
		libp2p.NATPortMap(),
		libp2p.EnableNATService(),
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			ddht, err = dht.New(ctx, h, dht.Mode(dht.ModeServer))
			return ddht, err
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	if err := ddht.Bootstrap(ctx); err != nil {
		return nil, fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	customDiscovery, err := discovery.NewDHTDiscovery(ctx, cancel, h, ddht, discovery.DHTConfig{})
	if err != nil {
		return nil, fmt.Errorf("failed to create custom discovery: %w", err)
	}

	customDiscovery.Advertise(ctx, AdvertiseName)

	ps, err := pubsub.NewGossipSub(ctx, h, pubsub.WithDiscovery(customDiscovery.GetRawDiscovery()))
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	ln := &Libp2pNetwork{
		host:         h,
		pubsub:       ps,
		selfPubKey:   selfPubKey,
		selfPrivKey:  selfPrivKey,
		peers:        make(map[peer.ID]*PeerInfo),
		blockStreams: make(map[peer.ID]network.Stream),
		voteStreams:  make(map[peer.ID]network.Stream),
		txStreams:    make(map[peer.ID]network.Stream),
		blockStore:   blockStore,
		maxPeers:     2,
	}

	ln.setupHandlers(bootstrapPeer)

	go func() {
		for {
			peerChan, err := customDiscovery.FindPeers(ctx, "blockchain", ln.maxPeers)
			if err != nil {
				logx.Error("DISCOVERY", "Failed to find peers:", err)
				time.Sleep(10 * time.Second)
				continue
			}

			for p := range peerChan {
				if p.ID == h.ID() || len(p.Addrs) == 0 {
					continue
				}

				if len(h.Network().Peers()) >= ln.maxPeers {
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

	logx.Info("NETWORK", fmt.Sprintf("Libp2p network started with ID: %s", h.ID().String()))
	for _, addr := range h.Addrs() {
		logx.Info("NETWORK", "Listening on:", addr.String())
	}

	return ln, nil
}

func (ln *Libp2pNetwork) setupHandlers(bootstrapPeer string) {
	ln.host.SetStreamHandler(NodeInfoProtocol, ln.handleNodeInfoStream)

	ln.SetupPubSubTopics()

	if bootstrapPeer != "" {
		addr, err := ma.NewMultiaddr(bootstrapPeer)
		if err != nil {
			logx.Error("NETWORK:SETUP", err.Error())
			return
		}

		info, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			logx.Error("NETWORK:SETUP", err.Error())
			return
		}

		go ln.RequestNodeInfo(bootstrapPeer, info)

		if len(ln.host.Network().Peers()) < ln.maxPeers {
			if err := ln.host.Connect(context.Background(), *info); err != nil {
				logx.Error("NETWORK:SETUP", "Failed to connect bootstrap peer: "+err.Error())
			} else {
				logx.Info("NETWORK:SETUP", "Connected to bootstrap peer "+info.ID.String())
			}
		}
	}

	logx.Info("NETWORK:SETUP", fmt.Sprintf("Libp2p network started with ID: %s", ln.host.ID().String()))
	logx.Info("NETWORK:SETUP", fmt.Sprintf("Listening on addresses: %v", ln.host.Addrs()))
	logx.Info("NETWORK:SETUP", fmt.Sprintf("Self public key: %s", ln.selfPubKey))
}
