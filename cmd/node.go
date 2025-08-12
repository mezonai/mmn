package cmd

import (
	"crypto/ed25519"
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
	"mmn/p2p"
	"mmn/poh"
	"mmn/validator"

	"github.com/spf13/cobra"
)

const (
	// Storage paths - using absolute paths
	fileBlockDir    = "./blockstore/blocks"
	rocksdbBlockDir = "blockstore/rocksdb"

	// Config paths
	configPath = "config/config.ini"
)

var (
	nodeName    string
	isbootstrap bool
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the blockchain node",
	Run: func(cmd *cobra.Command, args []string) {
		runNode(nodeName, isbootstrap)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringVarP(&nodeName, "node", "n", "node1", "The node to run")
	runCmd.Flags().BoolVar(&isbootstrap, "bootstrap", false, "Run as bootstrap node")
}

func runNode(currentNode string, isbootstrap bool) {
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
	cfg, err := loadConfiguration(currentNode)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize components with absolute paths
	bs, err := initializeRocksDBBlockstore(cfg.SelfNode.PubKey, absRocksdbBlockDir)
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

	// Load private key
	privKey, err := config.LoadEd25519PrivKey(cfg.SelfNode.PrivKeyPath)
	if err != nil {
		log.Fatalf("load private key: %w", err)
	}

	// Initialize network
	netClient, err := initializeNetwork(cfg.SelfNode, bs, privKey)
	if err != nil {
		log.Fatalf("Failed to initialize network: %v", err)
	}

	// Initialize mempool
	mp, err := initializeMempool(netClient)
	if err != nil {
		log.Fatalf("Failed to initialize mempool: %v", err)
	}

	// Initialize validator
	val, err := initializeValidator(cfg, pohService, recorder, mp, netClient, bs, ld, collector, privKey)
	if err != nil {
		log.Fatalf("Failed to initialize validator: %v", err)
	}

	// Start services
	startServices(cfg, netClient, ld, collector, val, bs, mp)

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
func initializeRocksDBBlockstore(pubKey string, rocksdbDir string) (blockstore.Store, error) {
	seed := []byte(pubKey)

	// Try RocksDB first
	rocksdb, err := blockstore.NewRocksDBStore(rocksdbDir, seed)
	if err != nil {
		return nil, fmt.Errorf("init rocksdb blockstore: %w", err)
	}
	log.Printf("Using RocksDB blockstore at %s", rocksdbDir)

	return rocksdb, nil
}

// Deprecated: use initializeBlockstore instead
func initializeFileBlockstore(pubKey string, fileBlockDir string) (blockstore.Store, error) {
	// File-based storage
	seed := []byte(pubKey)
	fbs, err := blockstore.NewBlockStore(fileBlockDir, seed)
	if err != nil {
		return nil, fmt.Errorf("init file blockstore: %w", err)
	}

	return fbs, nil
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
func initializeNetwork(self config.NodeConfig, bs blockstore.Store, privKey ed25519.PrivateKey) (*p2p.Libp2pNetwork, error) {
	// Prepare peer addresses (excluding self)
	libp2pNetwork, err := p2p.NewNetWork(
		self.PubKey,
		privKey,
		self.Libp2pAddr,
		self.BootStrapAddress,
		bs,
		isbootstrap,
	)

	return libp2pNetwork, err
}

// initializeMempool initializes the mempool
func initializeMempool(p2pClient *p2p.Libp2pNetwork) (*mempool.Mempool, error) {
	mempoolCfg, err := config.LoadMempoolConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load mempool config: %w", err)
	}

	mp := mempool.NewMempool(mempoolCfg.MaxTxs, p2pClient)
	return mp, nil
}

// initializeValidator initializes the validator
func initializeValidator(cfg *config.GenesisConfig, pohService *poh.PohService, recorder *poh.PohRecorder,
	mp *mempool.Mempool, p2pClient *p2p.Libp2pNetwork, bs blockstore.Store, ld *ledger.Ledger,
	collector *consensus.Collector, privKey ed25519.PrivateKey) (*validator.Validator, error) {

	validatorCfg, err := config.LoadValidatorConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load validator config: %w", err)
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
		leaderTimeoutLoopInterval, validatorCfg.BatchSize, p2pClient, bs, ld, collector,
	)
	val.Run()

	return val, nil
}

// startServices starts all network and API services
func startServices(cfg *config.GenesisConfig, p2pClient *p2p.Libp2pNetwork, ld *ledger.Ledger, collector *consensus.Collector,
	val *validator.Validator, bs blockstore.Store, mp *mempool.Mempool) {

	// Load private key for gRPC server
	privKey, err := config.LoadEd25519PrivKey(cfg.SelfNode.PrivKeyPath)
	if err != nil {
		log.Fatalf("Failed to load private key for gRPC server: %v", err)
	}

	// Start gRPC server
	grpcSrv := network.NewGRPCServer(
		cfg.SelfNode.GRPCAddr,
		map[string]ed25519.PublicKey{},
		fileBlockDir,
		ld,
		collector,
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
