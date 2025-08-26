package p2p

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mezonai/mmn/blockstore"
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
		maxPeers:           int(P2pMaxPeerConnections),
		activeSyncRequests: make(map[string]*SyncRequestInfo),
		syncRequests:       make(map[string]*SyncRequestTracker),
		authenticatedPeers: make(map[peer.ID]*AuthenticatedPeer),
		pendingChallenges:  make(map[peer.ID][]byte),
		allowlist:          make(map[peer.ID]bool),
		blacklist:          make(map[peer.ID]bool),
		allowlistEnabled:   false,
		blacklistEnabled:   true,
		syncCompleted:      false,
		activeSyncCount:    0,
		ctx:                ctx,
		cancel:             cancel,
	}

	ln.peerScoringManager = NewPeerScoringManager(ln, DefaultPeerScoringConfig())

	ln.InitializeAccessControl()

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
	ln.host.SetStreamHandler(AuthProtocol, ln.handleAuthStream)
	ln.host.SetStreamHandler(NodeInfoProtocol, ln.handleNodeInfoStream)

	ln.host.SetStreamHandler(RequestBlockSyncStream, ln.AuthMiddleware(ln.handleBlockSyncRequestStream))
	ln.host.SetStreamHandler(LatestSlotProtocol, ln.AuthMiddleware(ln.handleLatestSlotStream))

	ln.SetupPubSubTopics(ctx)
	ln.setupConnectionAuthentication(ctx)

	bootstrapConnected := false
	for _, bootstrapPeer := range bootstrapPeers {
		if bootstrapPeer == "" {
			continue
		}

		// Use DNS resolution for bootstrap addresses
		infos, err := discovery.ResolveAndParseMultiAddrs([]string{bootstrapPeer})
		if err != nil {
			logx.Error("NETWORK:SETUP", "Invalid bootstrap address: ", bootstrapPeer, " error: ", err)
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

func isProtocolNotSupportedError(err error) bool {
	if err == nil {
		return false
	}
	errorStr := err.Error()
	return strings.Contains(errorStr, "protocols not supported") ||
		strings.Contains(errorStr, "protocol not supported") ||
		strings.Contains(errorStr, "negotiate protocol")
}

func (ln *Libp2pNetwork) setupConnectionAuthentication(ctx context.Context) {
	ln.host.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(n network.Network, conn network.Conn) {
			peerID := conn.RemotePeer()
			logx.Info("AUTH:CONNECTION", "New connection from peer: ", peerID.String())

			ln.listMu.RLock()
			allowlistEmpty := ln.allowlistEnabled && len(ln.allowlist) == 0
			ln.listMu.RUnlock()

			if allowlistEmpty {
				logx.Info("AUTH:CONNECTION", "Allowing bootnode connection when allowlist is empty:", peerID.String())
			} else if !ln.IsAllowed(peerID) {
				logx.Info("AUTH:CONNECTION", "Rejecting connection from peer not allowed by access control:", peerID.String())
				conn.Close()
				return
			}

			ln.UpdatePeerScore(peerID, "connection", nil)

			exception.SafeGoWithPanic("Discovery", func() {
				authCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()

				if err := ln.InitiateAuthentication(authCtx, peerID); err != nil {
					if isProtocolNotSupportedError(err) {
						logx.Warn("AUTH:CONNECTION", "Peer doesn't support authentication protocol - this is normal for older peers")
					} else {
						ln.UpdatePeerScore(peerID, "auth_failure", nil)
					}
				} else {
					ln.UpdatePeerScore(peerID, "auth_success", nil)
					ln.AutoAddToAllowlistIfBootstrap(peerID)
				}
			})

		},
		DisconnectedF: func(n network.Network, conn network.Conn) {
			peerID := conn.RemotePeer()
			// Avoid penalizing disconnections for peers currently not allowed (e.g., blacklisted)
			if ln.IsAllowed(peerID) {
				ln.UpdatePeerScore(peerID, "disconnection", nil)
			}

			ln.authMu.Lock()
			delete(ln.authenticatedPeers, peerID)
			ln.authMu.Unlock()

			ln.challengeMu.Lock()
			delete(ln.pendingChallenges, peerID)
			ln.challengeMu.Unlock()
		},
	})
}

func (ln *Libp2pNetwork) Close() {
	if ln.peerScoringManager != nil {
		ln.peerScoringManager.Stop()
	}

	ln.cancel()
	ln.host.Close()
}

// IncrementActiveSyncCount increments the active sync count
func (ln *Libp2pNetwork) IncrementActiveSyncCount() {
	ln.activeSyncCountMu.Lock()
	defer ln.activeSyncCountMu.Unlock()
	ln.activeSyncCount++
	logx.Info("NETWORK:SYNC", "Active sync count incremented to:", ln.activeSyncCount)
}

// DecrementActiveSyncCount decrements the active sync count and enables allowlist if all syncs are done
func (ln *Libp2pNetwork) DecrementActiveSyncCount() {
	ln.activeSyncCountMu.Lock()
	defer ln.activeSyncCountMu.Unlock()

	if ln.activeSyncCount > 0 {
		ln.activeSyncCount--
		logx.Info("NETWORK:SYNC", "Active sync count decremented to:", ln.activeSyncCount)

		// If no more active syncs, enable allowlist
		if ln.activeSyncCount == 0 && !ln.IsAllowlistEnabled() {
			ln.EnableAllowlist(true)
			logx.Info("NETWORK:SYNC", "All syncs completed - Allowlist enabled for enhanced security")
		}
	}
}

// GetActiveSyncCount returns the current active sync count
func (ln *Libp2pNetwork) GetActiveSyncCount() int {
	ln.activeSyncCountMu.RLock()
	defer ln.activeSyncCountMu.RUnlock()
	return ln.activeSyncCount
}

func (ln *Libp2pNetwork) GetPeersConnected() int {
	return len(ln.peers)
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
	if err := json.Unmarshal(buf[:n], &msg); err != nil {
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

	err = ln.host.Connect(context.Background(), peerInfo)
	if err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", "Failed to connect to new peer: ", err)
	}
}
