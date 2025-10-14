package p2p

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"strings"
	"time"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/common"
	"github.com/mezonai/mmn/config"
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
	bootstrapPeers []string,
	blockStore store.BlockStore,
	txStore store.TxStore,
	pohCfg *config.PohConfig,
	isListener bool,
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

	h, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings(listenAddr, quicAddr),
		libp2p.EnableAutoRelayWithStaticRelays(relays),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
		libp2p.EnableNATService(),
		libp2p.NATPortMap(),
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

	ps, err := pubsub.NewGossipSub(ctx, h,
		pubsub.WithDiscovery(customDiscovery.GetRawDiscovery()),
		pubsub.WithMaxMessageSize(5*1024*1024),
		pubsub.WithValidateQueueSize(128),
		pubsub.WithPeerOutboundQueueSize(128),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	ln := &Libp2pNetwork{
		host:                     h,
		pubsub:                   ps,
		selfPubKey:               selfPubKey,
		selfPrivKey:              selfPrivKey,
		peers:                    make(map[peer.ID]*PeerInfo),
		bootstrapPeerIDs:         make(map[peer.ID]struct{}),
		blockStore:               blockStore,
		txStore:                  txStore,
		maxPeers:                 int(MaxPeers),
		activeSyncRequests:       make(map[string]*SyncRequestInfo),
		syncRequests:             make(map[string]*SyncRequestTracker),
		missingBlocksTracker:     make(map[uint64]*MissingBlockInfo),
		lastScannedSlot:          0,
		recentlyRequestedSlots:   make(map[uint64]time.Time),
		ctx:                      ctx,
		cancel:                   cancel,
		worldLatestSlot:          0,
		worldLatestPohSlot:       0,
		blockOrderingQueue:       make(map[uint64]*block.BroadcastedBlock),
		nextExpectedSlot:         0,
		blockQueueOrdering:       make(map[uint64]*block.BroadcastedBlock),
		nextExpectedSlotForQueue: 0,
		pohCfg:                   pohCfg,
		isListener:             isListener,
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

func (ln *Libp2pNetwork) GetPeersConnected() int {
	// Minus by 1 to exclude self node in the peer list
	return len(ln.host.Network().Peers()) - 1
}

func (ln *Libp2pNetwork) GetPeerInfo(peerID peer.ID) (*PeerInfo, bool) {
	if peerInfo, exists := ln.peers[peerID]; exists {
		return peerInfo, true
	}
	return nil, false
}

func (ln *Libp2pNetwork) GetLeaderForSlot(slot uint64) (peer.ID, bool) {
	if ln.leaderSchedule == nil {
		return "", false
	}

	leaderPubKey, exists := ln.leaderSchedule.LeaderAt(slot)
	if !exists {
		return "", false
	}

	leaderKeyBytes, err := common.DecodeBase58ToBytes(leaderPubKey)
	if err != nil {
		return "", false
	}

	for _, pid := range ln.host.Network().Peers() {
		pk := ln.host.Peerstore().PubKey(pid)
		if pk == nil {
			continue
		}
		raw, err := pk.Raw()
		if err != nil {
			continue
		}
		if bytes.Equal(raw, leaderKeyBytes) {
			return pid, true
		}
	}

	return "", false
}

// GetLeaderPublicKeyForSlot returns the public key of the leader for a given slot
// Returns empty string and false if no leader is assigned for the slot
func (ln *Libp2pNetwork) GetLeaderPublicKeyForSlot(slot uint64) (string, bool) {
	if ln.leaderSchedule == nil {
		return "", false
	}

	return ln.leaderSchedule.LeaderAt(slot)
}

// IsPeerConnected checks if a peer ID is currently connected
// Returns true if the peer is connected, false otherwise
func (ln *Libp2pNetwork) IsPeerConnected(peerID peer.ID) bool {
	// Check if peer is in the connected peers list
	connectedPeers := ln.host.Network().Peers()
	for _, connectedPeerID := range connectedPeers {
		if connectedPeerID == peerID {
			return true
		}
	}
	return false
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
func (ln *Libp2pNetwork) startCoreServices(ctx context.Context, withPubsub bool) {
	if ln.OnStartPoh != nil {
		ln.OnStartPoh()
	}
	if ln.OnStartValidator != nil {
		ln.OnStartValidator()
	}
	if withPubsub {
		ln.SetupPubSubTopics(ctx)
	}
	ln.setNodeReady()
}

func (ln *Libp2pNetwork) startLatestSlotRequestMechanism() {
	// Request latest slot after a delay to allow peers to connect
	exception.SafeGo("LatestSlotRequest(Initial)", func() {
		time.Sleep(3 * time.Second) // Wait for peers to connect
		ln.RequestLatestSlotFromPeers(ln.ctx)
	})

	// Periodic latest slot request every 30 seconds
	exception.SafeGo("LatestSlotRequest(Periodic)", func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ln.RequestLatestSlotFromPeers(ln.ctx)
			case <-ln.ctx.Done():
				return
			}
		}
	})
}
