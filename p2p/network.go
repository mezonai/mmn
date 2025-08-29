package p2p

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/mezonai/mmn/poh"
	"github.com/mezonai/mmn/store"

	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/discovery"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/snapshot"
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
	blockStore store.BlockStore,
) (*Libp2pNetwork, error) {

	privKey, err := crypto.UnmarshalEd25519PrivateKey(selfPrivKey)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal ed25519 private key: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
		host:                   h,
		pubsub:                 ps,
		selfPubKey:             selfPubKey,
		selfPrivKey:            selfPrivKey,
		peers:                  make(map[peer.ID]*PeerInfo),
		bootstrapPeerIDs:       make(map[peer.ID]struct{}),
		syncStreams:            make(map[peer.ID]network.Stream),
		blockStore:             blockStore,
		maxPeers:               int(MaxPeers),
		activeSyncRequests:     make(map[string]*SyncRequestInfo),
		syncRequests:           make(map[string]*SyncRequestTracker),
		missingBlocksTracker:   make(map[uint64]*MissingBlockInfo),
		lastScannedSlot:        0,
		recentlyRequestedSlots: make(map[uint64]time.Time),
		ctx:                    ctx,
		cancel:                 cancel,
		snapshotDownloader:     nil, // Will be initialized in setUpSyncNode
	}

	if err := ln.setUpSyncNode(ctx, bootstrapPeers); err != nil {
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

func (ln *Libp2pNetwork) setUpSyncNode(ctx context.Context, bootstrapPeers []string) error {
	ln.host.SetStreamHandler(NodeInfoProtocol, ln.handleNodeInfoStream)
	ln.host.SetStreamHandler(RequestBlockSyncStream, ln.handleBlockSyncRequestStream)
	ln.host.SetStreamHandler(LatestSlotProtocol, ln.handleLatestSlotStream)

	// Initialize snapshot downloader
	snapshotDir := "/data/snapshots"
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		logx.Error("NETWORK:SETUP", "Failed to create snapshot directory:", err)
	}

	// Get database provider from store if available
	var dbProvider db.DatabaseProvider
	if ln.blockStore != nil {
		if accountStore, ok := ln.blockStore.(store.AccountStore); ok {
			dbProvider = accountStore.GetDatabaseProvider()
		}
	}

	ln.snapshotDownloader = snapshot.NewSnapshotDownloader(dbProvider, snapshotDir)

	ln.setupSyncNodeTopics(ctx)

	// Start automatic snapshot discovery and sync
	go ln.startSnapshotDiscovery(ctx)

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

		// Record bootstrap peer ID for filtering later
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
	return len(ln.peers)
}

func (ln *Libp2pNetwork) handleNodeInfoStream(s network.Stream) {
	defer s.Close()

	buf := make([]byte, 2048)
	n, err := s.Read(buf)
	if err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", "Failed to read from peer: ", err)
		return
	}

	var msg map[string]interface{}
	if err := json.Unmarshal(buf[:n], &msg); err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", "Failed to unmarshal message: ", err)
		return
	}

	// Handle different message types
	if msgType, ok := msg["type"].(string); ok {
		switch msgType {
		case "snapshot_info_request":
			ln.handleSnapshotInfoRequest(msg, s)
			return
		case "snapshot_info_response":
			ln.handleSnapshotInfoResponse(msg)
			return
		case "snapshot_offer":
			ln.handleSnapshotOffer(msg)
			return
		}
	}

	// Handle regular node info (existing logic)
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

// handleSnapshotOffer handles snapshot offers from bootstrap nodes
func (ln *Libp2pNetwork) handleSnapshotOffer(msg map[string]interface{}) {
	logx.Info("NETWORK:SNAPSHOT", "Received snapshot offer from bootstrap node")

	// Check if we need snapshot sync
	if ln.needsSnapshotSync() {
		peerIDStr := msg["peer_id"].(string)
		peerID, err := peer.Decode(peerIDStr)
		if err != nil {
			logx.Error("NETWORK:SNAPSHOT", "Invalid peer ID in snapshot offer:", err)
			return
		}

		// Get peer address
		peerInfo := ln.host.Peerstore().PeerInfo(peerID)
		if len(peerInfo.Addrs) == 0 {
			logx.Error("NETWORK:SNAPSHOT", "No address found for peer:", peerID.String())
			return
		}

		// Extract IP and port for UDP
		peerAddr := ln.extractUDPAddr(peerInfo.Addrs[0])
		if peerAddr == "" {
			logx.Error("NETWORK:SNAPSHOT", "Could not extract UDP address from peer")
			return
		}

		logx.Info("NETWORK:SNAPSHOT", "Starting snapshot download from:", peerAddr)

		// Start snapshot download
		go ln.downloadSnapshotFromPeer(peerAddr, peerID.String())
	} else {
		logx.Info("NETWORK:SNAPSHOT", "Snapshot sync not needed")
	}
}

// needsSnapshotSync checks if the node needs to sync snapshot
func (ln *Libp2pNetwork) needsSnapshotSync() bool {
	// Check if we have any existing data
	if ln.blockStore != nil {
		if _, ok := ln.blockStore.(store.AccountStore); ok {
			// Check if we have any accounts
			// This is a simple heuristic - in production you might want more sophisticated logic
			return false // For now, assume we don't need sync
		}
	}

	// Check if snapshot file exists
	snapshotPath := "/data/snapshots/snapshot-latest.json"
	if _, err := os.Stat(snapshotPath); err == nil {
		return false // We already have a snapshot
	}

	return true
}

// extractUDPAddr extracts UDP address from multiaddr
func (ln *Libp2pNetwork) extractUDPAddr(addr ma.Multiaddr) string {
	// Extract IP and port from multiaddr
	ip := ""
	port := ""

	ma.ForEach(addr, func(c ma.Component) bool {
		if c.Protocol().Code == ma.P_IP4 || c.Protocol().Code == ma.P_IP6 {
			ip = c.Value()
		}
		if c.Protocol().Code == ma.P_TCP {
			port = c.Value()
		}
		return true
	})

	if ip != "" && port != "" {
		return fmt.Sprintf("%s:%s", ip, port)
	}

	return ""
}

// downloadSnapshotFromPeer downloads snapshot from a peer
func (ln *Libp2pNetwork) downloadSnapshotFromPeer(peerAddr, peerID string) {
	if ln.snapshotDownloader == nil {
		logx.Error("NETWORK:SNAPSHOT", "Snapshot downloader not initialized")
		return
	}

	// Start download
	task, err := ln.snapshotDownloader.DownloadSnapshotFromPeer(context.Background(), peerAddr, peerID, 0, 16*1024)
	if err != nil {
		logx.Error("NETWORK:SNAPSHOT", "Failed to start snapshot download:", err)
		return
	}

	logx.Info("NETWORK:SNAPSHOT", "Snapshot download started, task ID:", task.ID)

	// Monitor download progress
	go ln.monitorSnapshotDownload(task)
}

// monitorSnapshotDownload monitors the progress of snapshot download
func (ln *Libp2pNetwork) monitorSnapshotDownload(task *snapshot.DownloadTask) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			status, exists := ln.snapshotDownloader.GetDownloadStatus(task.ID)
			if !exists {
				logx.Info("NETWORK:SNAPSHOT", "Download task not found:", task.ID)
				return
			}

			switch status.Status {
			case snapshot.TransferStatusComplete:
				logx.Info("NETWORK:SNAPSHOT", "Snapshot download completed successfully")
				return
			case snapshot.TransferStatusFailed:
				logx.Error("NETWORK:SNAPSHOT", "Snapshot download failed")
				return
			case snapshot.TransferStatusCancelled:
				logx.Info("NETWORK:SNAPSHOT", "Snapshot download cancelled")
				return
			default:
				logx.Info("NETWORK:SNAPSHOT", "Download progress:", status.Progress, "%")
			}
		}
	}
}

func (ln *Libp2pNetwork) setupHandlers(ctx context.Context, bootstrapPeers []string) error {
	ln.SetupPubSubTopics(ctx)
	return nil
}

func (ln *Libp2pNetwork) IsNodeReady() bool {
	return ln.ready.Load()
}

func (ln *Libp2pNetwork) setNodeReady() {
	ln.ready.Store(true)
}

// SetApplyLeaderSchedule sets a callback for applying leader schedules at runtime
func (ln *Libp2pNetwork) SetApplyLeaderSchedule(fn func(*poh.LeaderSchedule)) {
	ln.applyLeaderSchedule = fn
}

// startSnapshotDiscovery continuously looks for peers with snapshots and syncs if needed
func (ln *Libp2pNetwork) startSnapshotDiscovery(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ln.needsSnapshotSync() {
				ln.discoverAndSyncSnapshot(ctx)
			}
		}
	}
}

// discoverAndSyncSnapshot finds peers with snapshots and initiates sync
func (ln *Libp2pNetwork) discoverAndSyncSnapshot(ctx context.Context) {
	logx.Info("NETWORK:SNAPSHOT", "Starting snapshot discovery...")

	// Get all connected peers
	peers := ln.host.Network().Peers()
	if len(peers) == 0 {
		logx.Info("NETWORK:SNAPSHOT", "No peers connected, waiting...")
		return
	}

	// Request snapshot info from all peers
	for _, peerID := range peers {
		if peerID == ln.host.ID() {
			continue // Skip self
		}

		// Check if we already have an active sync with this peer
		if ln.hasActiveSnapshotSync(peerID) {
			continue
		}

		go ln.requestSnapshotInfo(ctx, peerID)
	}
}

// requestSnapshotInfo requests snapshot information from a specific peer
func (ln *Libp2pNetwork) requestSnapshotInfo(ctx context.Context, peerID peer.ID) {
	// Create snapshot info request
	req := map[string]interface{}{
		"type":         "snapshot_info_request",
		"requester_id": ln.host.ID().String(),
	}

	data, err := json.Marshal(req)
	if err != nil {
		logx.Error("NETWORK:SNAPSHOT", "Failed to marshal snapshot info request:", err)
		return
	}

	// Send request via stream
	stream, err := ln.host.NewStream(ctx, peerID, NodeInfoProtocol)
	if err != nil {
		logx.Error("NETWORK:SNAPSHOT", "Failed to open stream for snapshot info request:", err)
		return
	}
	defer stream.Close()

	stream.Write(data)
	logx.Info("NETWORK:SNAPSHOT", "Sent snapshot info request to peer:", peerID.String())
}

// hasActiveSnapshotSync checks if we already have an active snapshot sync with a peer
func (ln *Libp2pNetwork) hasActiveSnapshotSync(peerID peer.ID) bool {
	if ln.snapshotDownloader == nil {
		return false
	}

	activeDownloads := ln.snapshotDownloader.GetActiveDownloads()
	for _, task := range activeDownloads {
		if task.PeerID == peerID.String() && task.Status == snapshot.TransferStatusActive {
			return true
		}
	}
	return false
}

// handleSnapshotInfoRequest handles requests for snapshot information
func (ln *Libp2pNetwork) handleSnapshotInfoRequest(msg map[string]interface{}, stream network.Stream) {
	logx.Info("NETWORK:SNAPSHOT", "Received snapshot info request")

	// Check if we have a snapshot to offer
	snapshotPath := "/data/snapshots/snapshot-latest.json"
	snapshotInfo := map[string]interface{}{
		"type":         "snapshot_info_response",
		"has_snapshot": false,
	}

	if _, err := os.Stat(snapshotPath); err == nil {
		// We have a snapshot, offer it
		snapshotInfo["has_snapshot"] = true
		snapshotInfo["snapshot_path"] = snapshotPath
		snapshotInfo["peer_id"] = ln.host.ID().String()

		// Get file info
		if fileInfo, err := os.Stat(snapshotPath); err == nil {
			snapshotInfo["file_size"] = fileInfo.Size()
			snapshotInfo["modified_time"] = fileInfo.ModTime().Unix()
		}
	}

	// Send response
	data, _ := json.Marshal(snapshotInfo)
	stream.Write(data)
	logx.Info("NETWORK:SNAPSHOT", "Sent snapshot info response")
}

// handleSnapshotInfoResponse handles responses to snapshot info requests
func (ln *Libp2pNetwork) handleSnapshotInfoResponse(msg map[string]interface{}) {
	logx.Info("NETWORK:SNAPSHOT", "Received snapshot info response")

	hasSnapshot, _ := msg["has_snapshot"].(bool)
	if !hasSnapshot {
		return
	}

	peerIDStr := msg["peer_id"].(string)
	peerID, err := peer.Decode(peerIDStr)
	if err != nil {
		logx.Error("NETWORK:SNAPSHOT", "Invalid peer ID in snapshot info response:", err)
		return
	}

	// Check if we need snapshot sync
	if ln.needsSnapshotSync() {
		logx.Info("NETWORK:SNAPSHOT", "Peer has snapshot, initiating download from:", peerID.String())

		// Get peer address for UDP
		peerInfo := ln.host.Peerstore().PeerInfo(peerID)
		if len(peerInfo.Addrs) == 0 {
			logx.Error("NETWORK:SNAPSHOT", "No address found for peer:", peerID.String())
			return
		}

		// Extract IP and port for UDP
		peerAddr := ln.extractUDPAddr(peerInfo.Addrs[0])
		if peerAddr == "" {
			logx.Error("NETWORK:SNAPSHOT", "Could not extract UDP address from peer")
			return
		}

		// Start snapshot download
		go ln.downloadSnapshotFromPeer(peerAddr, peerID.String())
	}
}
