package main

import (
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
	"mmn/network"
	"mmn/poh"
	"mmn/validator"
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
	bs, err := blockstore.NewBlockStore(blockDir)
	if err != nil {
		log.Fatalf("Failed to init blockstore: %v", err)
	}

	// --- Ledger ---
	ld := ledger.NewLedger()

	// --- Mempool ---
	mp := mempool.NewMempool(1000)

	// --- Collector ---
	collector := consensus.NewCollector(len(peers) + 1)

	// --- PoH ---
	seed := []byte(self.PubKey)
	hashesPerTick := uint64(5)
	ticksPerSlot := uint64(4)

	pohEngine := poh.NewPoh(seed, &hashesPerTick)
	pohEngine.Run()

	pohSchedule := config.ConvertLeaderSchedule(leaderSchedule)
	recorder := poh.NewPohRecorder(pohEngine, ticksPerSlot, self.PubKey, pohSchedule)
	tickInterval := 500 * time.Millisecond
	pohService := poh.NewPohService(recorder, tickInterval)
	pohService.Start()

	// --- Network (gRPC) ---
	netClient := network.NewGRPCClient(peerAddrs)
	pubKeys := make(map[string]ed25519.PublicKey)
	for _, n := range append(peers, self) {
		pub, err := hex.DecodeString(n.PubKey)
		if err == nil && len(pub) == ed25519.PublicKeySize {
			pubKeys[n.PubKey] = ed25519.PublicKey(pub)
		}
	}
	grpcSrv := network.NewGRPCServer(
		self.GRPCAddr,
		pubKeys,
		blockDir,
		ld,
		collector,
		netClient,
		self.PubKey,
		privKey,
		bs,
	)
	_ = grpcSrv // not used directly, but keeps server running

	// --- Validator ---
	pollInterval := 500 * time.Millisecond
	batchSize := 100
	val := validator.NewValidator(
		self.PubKey, privKey, recorder, pohService, pohSchedule, mp, ticksPerSlot,
		pollInterval, batchSize, netClient, bs, ld, collector,
	)
	val.Run()

	// --- API (for tx submission) ---
	apiSrv := api.NewAPIServer(mp, self.ListenAddr)
	apiSrv.Start()

	// --- Block forever ---
	select {}
}
