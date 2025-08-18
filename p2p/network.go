package p2p

import (
	"context"
	"crypto/ed25519"
	"fmt"

	"github.com/mezonai/mmn/blockstore"
	"github.com/mezonai/mmn/discovery"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/logx"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
)

func NewNetWork(
	selfPubKey string,
	selfPrivKey ed25519.PrivateKey,
	listenAddr string,
	bootstrapPeers []string,
	blockStore blockstore.Store,
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
		cancel()
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	if err := ddht.Bootstrap(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	customDiscovery, err := discovery.NewDHTDiscovery(ctx, cancel, h, ddht, discovery.DHTConfig{})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create custom discovery: %w", err)
	}

	customDiscovery.Advertise(ctx, AdvertiseName)

	ps, err := pubsub.NewGossipSub(ctx, h, pubsub.WithDiscovery(customDiscovery.GetRawDiscovery()))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	ln := &Libp2pNetwork{
		host:               h,
		pubsub:             ps,
		selfPubKey:         selfPubKey,
		selfPrivKey:        selfPrivKey,
		peers:              make(map[peer.ID]*PeerInfo),
		syncStreams:        make(map[peer.ID]network.Stream),
		blockStore:         blockStore,
		maxPeers:           int(MaxPeers),
		activeSyncRequests: make(map[string]*SyncRequestInfo),
		syncRequests:       make(map[string]*SyncRequestTracker),
		ctx:                ctx,
		cancel:             cancel,
	}

	if err := ln.setupHandlers(ctx, bootstrapPeers); err != nil {
		cancel()
		h.Close()
		return nil, fmt.Errorf("failed to setup handlers: %w", err)
	}

	exception.SafeGoWithPanic("Discovery", func() {
		ln.Discovery(customDiscovery, ctx, h)
	})

	logx.Info("NETWORK", fmt.Sprintf("Libp2p network started with ID: %s", h.ID().String()))
	for _, addr := range h.Addrs() {
		logx.Info("NETWORK", "Listening on:", addr.String())
	}

	return ln, nil
}

func (ln *Libp2pNetwork) setupHandlers(ctx context.Context, bootstrapPeers []string) error {
	ln.host.SetStreamHandler(NodeInfoProtocol, ln.handleNodeInfoStream)
	ln.host.SetStreamHandler(RequestBlockSyncStream, ln.handleBlockSyncRequestStream)
	ln.host.SetStreamHandler(LatestSlotProtocol, ln.handleLatestSlotStream)

	ln.SetupPubSubTopics(ctx)

	bootstrapConnected := false
	for _, bootstrapPeer := range bootstrapPeers {
		if bootstrapPeer == "" {
			continue
		}

		// Use DNS resolution for bootstrap addresses
		infos, err := discovery.ResolveAndParseMultiAddrs([]string{bootstrapPeer})
		if err != nil {
			logx.Error("NETWORK:SETUP", "Invalid bootstrap address: %s, error: %v", bootstrapPeer, err)
			continue
		}

		if len(infos) == 0 {
			logx.Error("NETWORK:SETUP", "No valid addresses resolved for:", bootstrapPeer)
			continue
		}

		info := infos[0] // Use the first resolved address
		if err := ln.host.Connect(ctx, info); err != nil {
			logx.Error("NETWORK:SETUP", "Failed to connect to bootstrap:", bootstrapPeer, err.Error())
			continue
		}

		logx.Info("NETWORK:SETUP", "Connected to bootstrap peer:", bootstrapPeer)
		bootstrapConnected = true

		exception.SafeGoWithPanic("RequestNodeInfo", func() {
			ln.RequestNodeInfo(bootstrapPeer, &info)
		})

		break
	}

	// If we have bootstrap peers configured but couldn't connect to any, return error
	if len(bootstrapPeers) > 0 && !bootstrapConnected {
		// Check if any bootstrap peer is non-empty
		hasNonEmptyBootstrap := false
		for _, peer := range bootstrapPeers {
			if peer != "" {
				hasNonEmptyBootstrap = true
				break
			}
		}
		if hasNonEmptyBootstrap {
			logx.Error("NETWORK:SETUP", "Failed to connect to any bootstrap peer. Stopping P2P server.")
			return fmt.Errorf("failed to connect to any bootstrap peer")
		}
	}

	logx.Info("NETWORK:SETUP", fmt.Sprintf("Libp2p network started with ID: %s", ln.host.ID().String()))
	logx.Info("NETWORK:SETUP", fmt.Sprintf("Listening on addresses: %v", ln.host.Addrs()))
	logx.Info("NETWORK:SETUP", fmt.Sprintf("Self public key: %s", ln.selfPubKey))
	return nil
}

// this func will call if node shutdown for now just cancle when error
func (ln *Libp2pNetwork) Close() {
	ln.cancel()
	ln.host.Close()
}
