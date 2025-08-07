package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"time"

	"mmn/api"
	"mmn/blockstore"
	"mmn/config"
	"mmn/consensus"
	"mmn/ledger"
	"mmn/mempool"

	// "mmn/network"  // Temporarily disabled during P2P migration
	"mmn/p2p"
	"mmn/poh"
	"mmn/validator"

	"github.com/libp2p/go-libp2p/core/peer"
)

func main() {
	current_node := flag.String("node", "node1", "The node to run")
	flag.Parse()
	// --- Load config from genesis.yml ---
	cfg, err := config.LoadGenesisConfig(fmt.Sprintf("config/genesis.%s.yml", *current_node))
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	self := cfg.SelfNode
	seed := []byte(self.PubKey)
	peers := cfg.PeerNodes
	leaderSchedule := cfg.LeaderSchedule

	// --- Prepare peer addresses (excluding self) ---
	peerAddrs := []string{}
	for _, p := range peers {
		if p.GRPCAddr != self.GRPCAddr {
			peerAddrs = append(peerAddrs, p.GRPCAddr)
		}
	}

	// --- Load private key from file (helper stub) ---
	privKey, err := config.LoadEd25519PrivKey(self.PrivKeyPath)
	if err != nil {
		log.Fatalf("Failed to load private key: %v", err)
	}

	// --- Blockstore ---
	blockDir := "./blockstore/blocks"
	bs, err := blockstore.NewBlockStore(blockDir, seed)
	if err != nil {
		log.Fatalf("Failed to init blockstore: %v", err)
	}

	// --- Ledger ---
	ld := ledger.NewLedger(cfg.Faucet.Address)

	// --- Collector ---
	collector := consensus.NewCollector(len(peers) + 1)

	// --- PoH ---
	hashesPerTick := uint64(5)
	ticksPerSlot := uint64(4)
	tickInterval := 500 * time.Millisecond
	pohAutoHashInterval := tickInterval / 5
	log.Printf("tickInterval: %v", tickInterval)
	log.Printf("pohAutoHashInterval: %v", pohAutoHashInterval)

	pohEngine := poh.NewPoh(seed, &hashesPerTick, pohAutoHashInterval)
	pohEngine.Run()

	pohSchedule := config.ConvertLeaderSchedule(leaderSchedule)
	recorder := poh.NewPohRecorder(pohEngine, ticksPerSlot, self.PubKey, pohSchedule)

	pohService := poh.NewPohService(recorder, tickInterval)
	pohService.Start()

	// --- Mempool (declare early for use in handlers) ---
	var mp *mempool.Mempool

	// --- Network (LibP2P) ---
	log.Printf("Setting up LibP2P networking...")

	// Create P2P configuration
	p2pConfig := &p2p.Config{
		ListenAddrs:    cfg.P2P.ListenAddrs,
		BootstrapPeers: cfg.P2P.BootstrapPeers,
		EnableDHT:      cfg.P2P.EnableDHT,
		EnablePubSub:   cfg.P2P.EnablePubSub,
	}

	// Create P2P service
	p2pService, err := p2p.NewP2PService(p2pConfig)
	if err != nil {
		log.Fatalf("Failed to create P2P service: %v", err)
	}
	defer p2pService.Close()

	// Create network adapter
	netClient := p2p.NewNetworkAdapter(p2pService)

	// Setup message handlers
	blockHandler := p2p.NewBlockMessageHandler(func(ctx context.Context, from peer.ID, blockData []byte) error {
		log.Printf("Received block from peer %s", from)
		// Handle block message - integrate with existing block processing
		return nil
	})

	voteHandler := p2p.NewVoteMessageHandler(func(ctx context.Context, from peer.ID, voteData []byte) error {
		log.Printf("Received vote from peer %s", from)
		// Handle vote message - integrate with existing vote processing
		return nil
	})

	txHandler := p2p.NewTxMessageHandler(func(ctx context.Context, from peer.ID, txData []byte) error {
		log.Printf("Received transaction from peer %s", from)
		// Handle transaction - add to mempool
		if mp != nil {
			mp.AddTx(txData, false) // Don't re-broadcast received tx
		}
		return nil
	})

	p2pService.SetMessageHandlers(blockHandler, voteHandler, txHandler)

	// Start P2P service
	if err := p2pService.Start(); err != nil {
		log.Fatalf("Failed to start P2P service: %v", err)
	}

	log.Printf("P2P Node started successfully!")
	log.Printf("Node ID: %s", netClient.GetNodeID())
	log.Printf("Listen addresses: %v", netClient.GetListenAddresses())
	log.Printf("Connected peers: %d", netClient.GetPeerCount())

	// Keep backwards compatibility for validator
	pubKeys := make(map[string]ed25519.PublicKey)
	for _, n := range append(peers, self) {
		pub, err := hex.DecodeString(n.PubKey)
		if err == nil && len(pub) == ed25519.PublicKeySize {
			pubKeys[n.PubKey] = ed25519.PublicKey(pub)
		}
	}

	// --- Initialize Mempool ---
	mp = mempool.NewMempool(1000, netClient)

	// --- Validator ---
	leaderBatchLoopInterval := tickInterval / 2
	log.Printf("leaderBatchLoopInterval: %v", leaderBatchLoopInterval)
	roleMonitorLoopInterval := tickInterval
	log.Printf("roleMonitorLoopInterval: %v", roleMonitorLoopInterval)
	batchSize := 100
	leaderTimeout := 50 * time.Millisecond
	leaderTimeoutLoopInterval := 5 * time.Millisecond

	val := validator.NewValidator(
		self.PubKey, privKey, recorder, pohService, pohSchedule, mp, ticksPerSlot,
		leaderBatchLoopInterval, roleMonitorLoopInterval, leaderTimeout, leaderTimeoutLoopInterval, batchSize, netClient, bs, ld, collector,
	)
	val.Run()

	// TODO: Migrate gRPC server to use P2P networking
	// For now, keep it disabled during P2P integration
	/*
		grpcSrv := network.NewGRPCServer(
			self.GRPCAddr,
			pubKeys,
			blockDir,
			ld,
			collector,
			netClient,
			self.PubKey,
			privKey,
			val,
			bs,
			mp,
		)
		_ = grpcSrv // not used directly, but keeps server running
	*/

	// --- Enhanced API (for tx submission and P2P monitoring) ---
	isBootstrap := len(cfg.P2P.BootstrapPeers) == 0 // Bootstrap if no bootstrap peers configured
	apiSrv := api.NewEnhancedAPIServer(mp, ld, netClient, self.ListenAddr, isBootstrap)
	apiSrv.Start()

	log.Printf("MMN Node started successfully!")
	log.Printf("Node ID: %s", p2pService.GetHostID())
	log.Printf("Listening on: %v", p2pService.GetListenAddresses())
	log.Printf("API server: %s", self.ListenAddr)
	log.Printf("Bootstrap mode: %v", isBootstrap)

	// --- Block forever ---
	select {}
}
