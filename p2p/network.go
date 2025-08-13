package p2p

import (
	"context"
	"crypto/ed25519"
	"fmt"

	"github.com/mezonai/mmn/blockstore"
	"github.com/mezonai/mmn/discovery"
	"github.com/mezonai/mmn/logx"

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
		host:         h,
		pubsub:       ps,
		selfPubKey:   selfPubKey,
		selfPrivKey:  selfPrivKey,
		peers:        make(map[peer.ID]*PeerInfo),
		blockStreams: make(map[peer.ID]network.Stream),
		voteStreams:  make(map[peer.ID]network.Stream),
		txStreams:    make(map[peer.ID]network.Stream),
		blockStore:   blockStore,
		maxPeers:     int(MaxPeers),
		ctx:          ctx,
		cancel:       cancel,
	}

	ln.setupHandlers(ctx, bootstrapPeers) // ✅ truyền ctx xuống
	go ln.Discovery(customDiscovery, ctx, h)

	logx.Info("NETWORK", fmt.Sprintf("Libp2p network started with ID: %s", h.ID().String()))
	for _, addr := range h.Addrs() {
		logx.Info("NETWORK", "Listening on:", addr.String())
	}

	return ln, nil
}

func (ln *Libp2pNetwork) setupHandlers(ctx context.Context, bootstrapPeers []string) {
	ln.host.SetStreamHandler(NodeInfoProtocol, ln.handleNodeInfoStream)
	ln.SetupPubSubTopics(ctx)

	for _, bootstrapPeer := range bootstrapPeers {
		if bootstrapPeer == "" {
			continue
		}

		addr, err := ma.NewMultiaddr(bootstrapPeer)
		if err != nil {
			logx.Error("NETWORK:SETUP", "Invalid bootstrap address:", bootstrapPeer, err.Error())
			continue
		}

		info, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			logx.Error("NETWORK:SETUP", "Failed to parse peer info:", bootstrapPeer, err.Error())
			continue
		}

		go ln.RequestNodeInfo(bootstrapPeer, info)
		// go ln.RequestBlockSync(ln.blockStore.LatestSlot() + 1)

		break
	}

	logx.Info("NETWORK:SETUP", fmt.Sprintf("Libp2p network started with ID: %s", ln.host.ID().String()))
	logx.Info("NETWORK:SETUP", fmt.Sprintf("Listening on addresses: %v", ln.host.Addrs()))
	logx.Info("NETWORK:SETUP", fmt.Sprintf("Self public key: %s", ln.selfPubKey))
}

// this func will call if node shutdown for now just cancle when error
func (ln *Libp2pNetwork) Close() {
	ln.cancel()
	ln.host.Close()
}
