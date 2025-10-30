package p2p

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/faucet"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/poh"
	"github.com/mezonai/mmn/store"

	"github.com/mezonai/mmn/discovery"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/logx"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	rclient "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
)

func NewNetWork(
	selfPubKey string,
	selfPrivKey ed25519.PrivateKey,
	listenAddr string,
	p2pPort string,
	publicIP string,
	bootstrapPeers []string,
	blockStore store.BlockStore,
	txStore store.TxStore,
	pohCfg *config.PohConfig,
	isListener bool,
	multisigStore store.MultisigFaucetStore,
) (*Libp2pNetwork, error) {
	privKey, err := crypto.UnmarshalEd25519PrivateKey(selfPrivKey)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal ed25519 private key: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	var ddht *dht.IpfsDHT

	var relays []peer.AddrInfo
	for _, bootstrapPeer := range bootstrapPeers {
		if bootstrapPeer == "" {
			continue
		}
		infos, err := discovery.ResolveAndParseMultiAddrs([]string{bootstrapPeer})
		if err != nil {
			logx.Error("NETWORK:SETUP", "Invalid bootstrap address for static relay:", bootstrapPeer, ", error:", err)
			continue
		}
		if len(infos) > 0 {
			relays = append(relays, infos[0])
		}
	}

	quicAddr := strings.Replace(listenAddr, "/tcp/", "/udp/", 1) + "/quic-v1"

	publicTcpAddr, publicQuicAddr := createPublicAddresses(p2pPort, publicIP)

	h, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings(listenAddr, quicAddr),
		libp2p.EnableAutoRelayWithStaticRelays(relays),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
		libp2p.EnableNATService(),
		libp2p.NATPortMap(),
		libp2p.ForceReachabilityPublic(),
		libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			if publicTcpAddr != nil && publicQuicAddr != nil {
				addrs = append(addrs, publicTcpAddr, publicQuicAddr)
			}
			return addrs
		}),
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

	_, err = customDiscovery.Advertise(ctx, AdvertiseName)
	if err != nil {
		logx.Warn("NETWORK:SETUP", "Failed to advertise:", err)
	}

	ps, err := pubsub.NewGossipSub(ctx, h,
		pubsub.WithDiscovery(customDiscovery.GetRawDiscovery()),
		pubsub.WithMaxMessageSize(5*1024*1024),
		pubsub.WithValidateQueueSize(128),
		pubsub.WithPeerOutboundQueueSize(128),
		// Ensure publishes are forwarded even if not in mesh yet
		pubsub.WithFloodPublish(true),
		// Help small networks discover pubsub peers quickly
		pubsub.WithPeerExchange(true),
		// Ensure we forward directly to bootstrap/relay peers
		pubsub.WithDirectPeers(relays),
	)
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
		bootstrapPeerIDs:   make(map[peer.ID]struct{}),
		blockStore:         blockStore,
		txStore:            txStore,
		maxPeers:           int(MaxPeers),
		syncRequests:       make(map[string]*SyncRequestTracker),
		ctx:                ctx,
		cancel:             cancel,
		worldLatestSlot:    0,
		worldLatestPohSlot: 0,
		blockOrderingQueue: make(map[uint64]*block.BroadcastedBlock),
		nextExpectedSlot:   0,
		pohCfg:             pohCfg,
		isListener:         isListener,
		multisigStore:      multisigStore,
	}

	if err := ln.setupHandlers(ctx, bootstrapPeers); err != nil {
		cancel()
		closeErr := h.Close()
		if closeErr != nil {
			logx.Error("NETWORK:SETUP", "Failed to close host:", closeErr)
		}
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
	ln.host.SetStreamHandler(CheckpointProtocol, ln.handleCheckpointStream)

	// Start latest slot request mechanism
	ln.startLatestSlotRequestMechanism()
	bootstrapConnected := false
	for _, bootstrapPeer := range bootstrapPeers {
		if bootstrapPeer == "" {
			continue
		}

		// Use DNS resolution for bootstrap addresses
		infos, err := discovery.ResolveAndParseMultiAddrs([]string{bootstrapPeer})
		if err != nil {
			logx.Error("NETWORK:SETUP", "Invalid bootstrap address:", bootstrapPeer, ", error:", err)
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

		ln.bootstrapPeerIDs[info.ID] = struct{}{}
		ln.reserveViaPeer(ctx, info)
		exception.SafeGoWithPanic("RequestNodeInfo", func() {
			err := ln.RequestNodeInfo(bootstrapPeer, &info)
			if err != nil {
				logx.Error("NETWORK:REQUEST NODE INFO", "Failed to request node info:", err)
			}
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

	ln.startRelayReservationMaintainer()
	return nil
}

// reserveViaPeer tries to  a circuit v2 relay slot via the given peer
func (ln *Libp2pNetwork) reserveViaPeer(ctx context.Context, info peer.AddrInfo) {
	ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := rclient.Reserve(ctx2, ln.host, info); err != nil {
		logx.Warn("RELAYER", "Bootstrap relay reservation failed:", err)
		return
	}
}

func (ln *Libp2pNetwork) startRelayReservationMaintainer() {
	exception.SafeGo("RelayReservationMaintainer", func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				for pid := range ln.bootstrapPeerIDs {
					pi := ln.host.Peerstore().PeerInfo(pid)
					if pi.ID == "" {
						continue
					}
					ctx2, cancel := context.WithTimeout(ln.ctx, 10*time.Second)
					if _, err := rclient.Reserve(ctx2, ln.host, pi); err != nil {
						logx.Warn("RELAYER", "Periodic relay reservation failed:", err)
					}
					cancel()
				}

			case <-ln.ctx.Done():
				return
			}
		}
	})
}

func createPublicAddresses(p2pPort string, publicIP string) (ma.Multiaddr, ma.Multiaddr) {
	port, err := strconv.Atoi(p2pPort)
	if err != nil {
		return nil, nil
	}

	publicTcpAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d", publicIP, port))
	publicQuicAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/udp/%d/quic-v1", publicIP, port))

	return publicTcpAddr, publicQuicAddr
}

// this func will call if node shutdown for now just cancle when error
func (ln *Libp2pNetwork) Close() {
	ln.cancel()
	err := ln.host.Close()
	if err != nil {
		logx.Error("NETWORK:CLOSE", "Failed to close host: ", err)
	}
}

func (ln *Libp2pNetwork) GetPeersConnected() int {
	// Minus by 1 to exclude self node in the peer list
	return len(ln.host.Network().Peers()) - 1
}

func (ln *Libp2pNetwork) handleNodeInfoStream(s network.Stream) {
	defer s.Close()

	buf := make([]byte, 2048)
	n, err := s.Read(buf)
	if err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", "Failed to read from bootstrap: ", err)
		return
	}

	var msg map[string]interface{}
	if err := jsonx.Unmarshal(buf[:n], &msg); err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", "Failed to unmarshal peer info: ", err)
		return
	}

	newPeerIDStr := msg["new_peer_id"].(string)
	newPeerID, err := peer.Decode(newPeerIDStr)
	if err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", "Invalid peer ID: ", newPeerIDStr)
		return
	}

	addrStrs := msg["addrs"].([]interface{})
	var addrs []ma.Multiaddr
	for _, a := range addrStrs {
		maddr, err := ma.NewMultiaddr(a.(string))
		if err == nil {
			addrs = append(addrs, maddr)
		}
	}

	peerInfo := peer.AddrInfo{
		ID:    newPeerID,
		Addrs: addrs,
	}

	for _, maddr := range addrs {
		addrStr := maddr.String()
		idx := strings.Index(addrStr, "/p2p-circuit/p2p/")
		if idx <= 0 {
			continue
		}
		hopStr := addrStr[:idx]
		hopMaddr, err := ma.NewMultiaddr(hopStr)
		if err != nil {
			continue
		}
		relayInfo, err := peer.AddrInfoFromP2pAddr(hopMaddr)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if _, err := rclient.Reserve(ctx, ln.host, *relayInfo); err != nil {
			logx.Warn("RELAYER", "Reserve via hop failed:", err)
		} else {
			logx.Info("RELAYER", "Reserved via hop relay:", relayInfo.ID.String())
		}
		cancel()
		break
	}

	err = ln.host.Connect(context.Background(), peerInfo)
	if err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", peerInfo.Addrs, "Failed to connect to new peer:", err)
		return
	}
}

func (ln *Libp2pNetwork) GetHostId() peer.ID {
	return ln.host.ID()
}

// ApplyLeaderSchedule stores the schedule locally for leader checks inside p2p
func (ln *Libp2pNetwork) ApplyLeaderSchedule(ls *poh.LeaderSchedule) {
	ln.leaderSchedule = ls
}

func (ln *Libp2pNetwork) IsNodeReady() bool {
	return ln.ready.Load()
}

func (ln *Libp2pNetwork) setNodeReady() {
	ln.ready.Store(true)
}

// startCoreServices starts PoH and Validator (if callbacks provided), sets up pubsub topics, and marks node ready
func (ln *Libp2pNetwork) startCoreServices(ctx context.Context) {
	if ln.OnStartPoh != nil {
		ln.OnStartPoh()
	}
	if ln.OnStartValidator != nil {
		ln.OnStartValidator()
	}

	ln.SetupPubSubTopics(ctx)

	ln.setNodeReady()

	err := ln.topicRequesInitFaucetConfig.Publish(ctx, []byte{})
	if err != nil {
		logx.Error("NETWORK:START CORE SERVICES", "Failed to publish block sync request to start block sync: ", err)
	}

}

func (ln *Libp2pNetwork) startLatestSlotRequestMechanism() {
	// Request latest slot after a delay to allow peers to connect
	exception.SafeGo("LatestSlotRequest(Initial)", func() {
		time.Sleep(3 * time.Second) // Wait for peers to connect
		_, err := ln.RequestLatestSlotFromPeers(ln.ctx)
		if err != nil {
			logx.Error("NETWORK:LATEST SLOT", "Failed to request latest slot from peers: ", err)
		}
	})

	// Periodic latest slot request every 30 seconds
	exception.SafeGo("LatestSlotRequest(Periodic)", func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, err := ln.RequestLatestSlotFromPeers(ln.ctx)
				if err != nil {
					logx.Error("NETWORK:LATEST SLOT", "Failed to request latest slot from peers: ", err)
				}
			case <-ln.ctx.Done():
				return
			}
		}
	})
}

func (ln *Libp2pNetwork) SetFaucetCallbacks(
	HandleFaucetWhitelist func(msg *faucet.FaucetSyncWhitelistMessage) error,
	HandleFaucetConfig func(msg *faucet.FaucetSyncConfigMessage) error,
	HandleFaucetMultisigTx func(msg *faucet.FaucetSyncTransactionMessage) error,
	VerifyVote func(voteMsg *faucet.RequestFaucetVoteMessage) bool,
	GetFaucetConfig func() *faucet.RequestInitFaucetConfigMessage,
	HandleInitFaucetConfig func(msg *faucet.RequestInitFaucetConfigMessage) error,
	FaucetVoteHandler func(txHash string, voterID peer.ID, approve bool),
) {
	ln.HandleFaucetWhitelist = HandleFaucetWhitelist
	ln.HandleFaucetConfig = HandleFaucetConfig
	ln.HandleFaucetMultisigTx = HandleFaucetMultisigTx
	ln.VerifyVote = VerifyVote
	ln.GetFaucetConfig = GetFaucetConfig
	ln.HandleInitFaucetConfig = HandleInitFaucetConfig
	ln.OnFaucetVote = FaucetVoteHandler

}
