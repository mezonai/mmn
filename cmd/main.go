package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

const (
	// Storage paths - using absolute paths
	fileBlockDir    = "./blockstore/blocks"
	rocksdbBlockDir = "blockstore/rocksdb"

	// Config paths
	configPath = "config/config.ini"
)

func main() {
	nodeName := flag.String("node", "node1", "The node to run")
	flag.Parse()

	// Get current directory and create absolute path
	currentDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get current directory: %v", err)
	}

	// Create absolute path for storage
	absRocksdbBlockDir := filepath.Join(currentDir, rocksdbBlockDir)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(absRocksdbBlockDir, 0755); err != nil {
		log.Printf("Warning: Failed to create directory %s: %v", absRocksdbBlockDir, err)
	}

	// Load configuration
	cfg, err := loadConfiguration(*nodeName)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize components with absolute paths
	bs, err := initializeBlockstore(cfg.SelfNode.PubKey, absRocksdbBlockDir)
	if err != nil {
		log.Fatalf("Failed to initialize blockstore: %v", err)
	}

	ld := ledger.NewLedger(cfg.Faucet.Address)
	collector := consensus.NewCollector(len(cfg.PeerNodes) + 1)

	// Initialize PoH components
	_, pohService, recorder, err := initializePoH(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize PoH: %v", err)
	}

	// Initialize network
	netClient, pubKeys, err := initializeNetwork(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize network: %v", err)
	}

	// Initialize mempool
	mp, err := initializeMempool(netClient)
	if err != nil {
		log.Fatalf("Failed to initialize mempool: %v", err)
	}

	// Initialize validator
	val, err := initializeValidator(cfg, pohService, recorder, mp, netClient, bs, ld, collector)
	if err != nil {
		log.Fatalf("Failed to initialize validator: %v", err)
	}

	// Start services
	startServices(cfg, netClient, pubKeys, ld, collector, val, bs, mp)

	// Block forever
	select {}
}

// loadConfiguration loads all configuration files
func loadConfiguration(nodeName string) (*config.GenesisConfig, error) {
	cfg, err := config.LoadGenesisConfig(fmt.Sprintf("config/genesis.%s.yml", nodeName))
	if err != nil {
		return nil, fmt.Errorf("load genesis config: %w", err)
	}
	return cfg, nil
}

// initializeBlockstore initializes the block storage backend
func initializeBlockstore(pubKey string, rocksdbDir string) (blockstore.Store, error) {
	seed := []byte(pubKey)

	// Try RocksDB first
	rocksdb, err := blockstore.NewRocksDBStore(rocksdbDir, seed)
	if err != nil {
		return nil, fmt.Errorf("init rocksdb blockstore: %w", err)
	}
	log.Printf("Using RocksDB blockstore at %s", rocksdbDir)

	return rocksdb, nil
	// File-based storage
	// log.Printf("RocksDB unavailable; falling back to file-based blockstore")
	// fbs, err := blockstore.NewBlockStore(fileBlockDir, seed)
	// if err != nil {
	// 	return nil, fmt.Errorf("init file blockstore: %w", err)
	// }

	// return fbs, nil
}

// initializePoH initializes Proof of History components
func initializePoH(cfg *config.GenesisConfig) (*poh.Poh, *poh.PohService, *poh.PohRecorder, error) {
	pohCfg, err := config.LoadPohConfig(configPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load PoH config: %w", err)
	}

	seed := []byte(cfg.SelfNode.PubKey)
	hashesPerTick := pohCfg.HashesPerTick
	ticksPerSlot := pohCfg.TicksPerSlot
	tickInterval := time.Duration(pohCfg.TickIntervalMs) * time.Millisecond
	pohAutoHashInterval := tickInterval / 5

	log.Printf("PoH config: tickInterval=%v, autoHashInterval=%v", tickInterval, pohAutoHashInterval)

	pohEngine := poh.NewPoh(seed, &hashesPerTick, pohAutoHashInterval)
	pohEngine.Run()

	pohSchedule := config.ConvertLeaderSchedule(cfg.LeaderSchedule)
	recorder := poh.NewPohRecorder(pohEngine, ticksPerSlot, cfg.SelfNode.PubKey, pohSchedule)

	pohService := poh.NewPohService(recorder, tickInterval)
	pohService.Start()

	return pohEngine, pohService, recorder, nil
}

// initializeNetwork initializes network components
func initializeNetwork(cfg *config.GenesisConfig) (*network.GRPCClient, map[string]ed25519.PublicKey, error) {
	// Prepare peer addresses (excluding self)
	peerAddrs := make([]string, 0, len(cfg.PeerNodes))
	for _, p := range cfg.PeerNodes {
		if p.GRPCAddr != cfg.SelfNode.GRPCAddr {
			peerAddrs = append(peerAddrs, p.GRPCAddr)
		}
	}

	netClient := network.NewGRPCClient(peerAddrs)

	// Build public key map
	pubKeys := make(map[string]ed25519.PublicKey)
	allNodes := append(cfg.PeerNodes, cfg.SelfNode)

	for _, n := range allNodes {
		pub, err := hex.DecodeString(n.PubKey)
		if err == nil && len(pub) == ed25519.PublicKeySize {
			pubKeys[n.PubKey] = ed25519.PublicKey(pub)
		}
	}

	return netClient, pubKeys, nil
}

// initializeMempool initializes the mempool
func initializeMempool(netClient *network.GRPCClient) (*mempool.Mempool, error) {
	mempoolCfg, err := config.LoadMempoolConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load mempool config: %w", err)
	}

	mp := mempool.NewMempool(mempoolCfg.MaxTxs, netClient)
	return mp, nil
}

// initializeValidator initializes the validator
func initializeValidator(cfg *config.GenesisConfig, pohService *poh.PohService, recorder *poh.PohRecorder,
	mp *mempool.Mempool, netClient *network.GRPCClient, bs blockstore.Store, ld *ledger.Ledger,
	collector *consensus.Collector) (*validator.Validator, error) {

	validatorCfg, err := config.LoadValidatorConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load validator config: %w", err)
	}

	// Load private key
	privKey, err := config.LoadEd25519PrivKey(cfg.SelfNode.PrivKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load private key: %w", err)
	}

	// Calculate intervals
	pohCfg, _ := config.LoadPohConfig(configPath) // Already loaded in initializePoH
	tickInterval := time.Duration(pohCfg.TickIntervalMs) * time.Millisecond
	leaderBatchLoopInterval := tickInterval / 2
	roleMonitorLoopInterval := tickInterval
	leaderTimeout := time.Duration(validatorCfg.LeaderTimeout) * time.Millisecond
	leaderTimeoutLoopInterval := time.Duration(validatorCfg.LeaderTimeoutLoopInterval) * time.Millisecond

	log.Printf("Validator config: batchLoopInterval=%v, monitorLoopInterval=%v",
		leaderBatchLoopInterval, roleMonitorLoopInterval)

	val := validator.NewValidator(
		cfg.SelfNode.PubKey, privKey, recorder, pohService,
		config.ConvertLeaderSchedule(cfg.LeaderSchedule), mp, pohCfg.TicksPerSlot,
		leaderBatchLoopInterval, roleMonitorLoopInterval, leaderTimeout,
		leaderTimeoutLoopInterval, validatorCfg.BatchSize, netClient, bs, ld, collector,
	)
	val.Run()

	return val, nil
}

// startServices starts all network and API services
func startServices(cfg *config.GenesisConfig, netClient *network.GRPCClient,
	pubKeys map[string]ed25519.PublicKey, ld *ledger.Ledger, collector *consensus.Collector,
	val *validator.Validator, bs blockstore.Store, mp *mempool.Mempool) {

	// Load private key for gRPC server
	privKey, err := config.LoadEd25519PrivKey(cfg.SelfNode.PrivKeyPath)
	if err != nil {
		log.Fatalf("Failed to load private key for gRPC server: %v", err)
	}

	// Start gRPC server
	grpcSrv := network.NewGRPCServer(
		cfg.SelfNode.GRPCAddr,
		pubKeys,
		fileBlockDir,
		ld,
		collector,
		netClient,
		cfg.SelfNode.PubKey,
		privKey,
		val,
		bs,
		mp,
	)
	_ = grpcSrv // Keep server running

	// Start API server
	apiSrv := api.NewAPIServer(mp, ld, cfg.SelfNode.ListenAddr)
	apiSrv.Start()
}
