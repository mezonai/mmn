package cmd

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/mezonai/mmn/api"
	"github.com/mezonai/mmn/blockstore"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/network"
	"github.com/mezonai/mmn/p2p"
	"github.com/mezonai/mmn/poh"
	"github.com/mezonai/mmn/validator"

	"github.com/spf13/cobra"
)

const (
	// Storage paths - using absolute paths
	fileBlockDir    = "./blockstore/blocks"
	rocksdbBlockDir = "blockstore/rocksdb"
	// Config paths
	configPath    = "config/config.ini"
	faucetAddress = "0d1dfad29c20c13dccff213f52d2f98a395a0224b5159628d2bdb077cf4026a7"
)

var (
	privKeyPath        string
	listenAddr         string
	p2pPort            string
	bootstrapAddresses []string
	grpcAddr           string
	// faucet
	faucetAmount string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the blockchain node",
	Run: func(cmd *cobra.Command, args []string) {
		runNode()

	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringVar(&privKeyPath, "privkey-path", "", "Path to private key file")
	runCmd.Flags().StringVar(&listenAddr, "listen-addr", ":8001", "Listen address for API server :<port>")
	runCmd.Flags().StringVar(&listenAddr, "grpc-addr", ":9001", "Listen address for Grpc server :<port>")
	runCmd.Flags().StringVar(&p2pPort, "p2p-port", "10001", "LibP2P listen multiaddress /ip4/0.0.0.0/tcp/<port>")
	runCmd.Flags().StringArrayVar(&bootstrapAddresses, "bootstrap-addresses", []string{}, "List of bootstrap peer multiaddresses")
	runCmd.Flags().StringVar(&faucetAmount, "faucet-amount", "2000000000", "Faucet Amount")
}

func runNode() {
	// Handle Docker stop or Ctrl+C
	ctx, cancel := context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Get current directory and create absolute path
	currentDir, err := os.Getwd()
	if err != nil {
		logx.Warn("Failed to get current directory: %v", err)
	}

	// Create absolute path for storage
	absRocksdbBlockDir := filepath.Join(currentDir, rocksdbBlockDir)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(absRocksdbBlockDir, 0755); err != nil {
		logx.Warn("Warning: Failed to create directory %s: %v", absRocksdbBlockDir, err)
	}

	pubKey, err := config.LoadPubKeyFromPriv(privKeyPath)
	if err != nil {
		logx.Error("Public Key", err.Error())
	}
	// Initialize components with absolute paths
	bs, err := initializeRocksDBBlockstore(pubKey, absRocksdbBlockDir)
	if err != nil {
		logx.Warn("Failed to initialize blockstore: %v", err)
	}

	ld := ledger.NewLedger(faucetAddress)

	faucetAmountNumber, err := strconv.ParseUint(faucetAmount, 10, 64)
	if err != nil {
		logx.Error("Faucet Amount Not A Number", err)
		return
	}

	if privKeyPath == "" {
		logx.Error("Private Key Empty")
		return
	}

	Libp2pAddr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%s", p2pPort)

	cfg := config.GenesisConfig{
		SelfNode: config.NodeConfig{
			PubKey:             pubKey,
			PrivKeyPath:        privKeyPath,
			ListenAddr:         listenAddr,
			Libp2pAddr:         Libp2pAddr,
			GRPCAddr:           grpcAddr,
			BootStrapAddresses: bootstrapAddresses,
		},
		LeaderSchedule: config.LeaderSchedules,
		Faucet: config.Faucet{
			Address: faucetAddress,
			Amount:  faucetAmountNumber,
		},
	}

	// Initialize PoH components
	_, pohService, recorder, err := initializePoH(&cfg)
	if err != nil {
		log.Fatalf("Failed to initialize PoH: %v", err)
	}

	// Load private key
	privKey, err := config.LoadEd25519PrivKey(privKeyPath)
	if err != nil {
		log.Fatalf("load private key: %w", err)
	}

	// Initialize network
	libP2pClient, err := initializeNetwork(cfg.SelfNode, bs, privKey)
	if err != nil {
		log.Fatalf("Failed to initialize network: %v", err)
	}

	// Initialize mempool
	mp, err := initializeMempool(libP2pClient)
	if err != nil {
		log.Fatalf("Failed to initialize mempool: %v", err)
	}

	collector := consensus.NewCollector(libP2pClient.GetPeersConnected() + 1)

	libP2pClient.SetupCallbacks(ld, privKey, cfg.SelfNode, bs, collector, mp)

	// Initialize validator
	val, err := initializeValidator(&cfg, pohService, recorder, mp, libP2pClient, bs, ld, collector, privKey)
	if err != nil {
		log.Fatalf("Failed to initialize validator: %v", err)
	}

	// Start services
	startServices(&cfg, libP2pClient, ld, collector, val, bs, mp)

	go func() {
		<-sigCh
		log.Println("Shutting down node...")
		// for now just shutdown p2p network
		libP2pClient.Close()
		cancel()
	}()

	//  block until cancel
	<-ctx.Done()

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
		self.BootStrapAddresses,
		bs,
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

func mergeWithDefaultConfig(defaultCfg, loadedCfg *config.GenesisConfig) *config.GenesisConfig {
	if loadedCfg.Faucet.Address == "" {
		loadedCfg.Faucet.Address = defaultCfg.Faucet.Address
	}
	if loadedCfg.Faucet.Amount == 0 {
		loadedCfg.Faucet.Amount = defaultCfg.Faucet.Amount
	}
	if loadedCfg.SelfNode.PubKey == "" {
		loadedCfg.SelfNode.PubKey = defaultCfg.SelfNode.PubKey
	}
	if loadedCfg.SelfNode.PrivKeyPath == "" {
		loadedCfg.SelfNode.PrivKeyPath = defaultCfg.SelfNode.PrivKeyPath
	}
	if loadedCfg.SelfNode.ListenAddr == "" {
		loadedCfg.SelfNode.ListenAddr = defaultCfg.SelfNode.ListenAddr
	}
	if loadedCfg.SelfNode.Libp2pAddr == "" {
		loadedCfg.SelfNode.Libp2pAddr = defaultCfg.SelfNode.Libp2pAddr
	}
	if loadedCfg.SelfNode.GRPCAddr == "" {
		loadedCfg.SelfNode.GRPCAddr = defaultCfg.SelfNode.GRPCAddr
	}
	if len(loadedCfg.SelfNode.BootStrapAddresses) == 0 {
		loadedCfg.SelfNode.BootStrapAddresses = defaultCfg.SelfNode.BootStrapAddresses
	}
	loadedCfg.LeaderSchedule = defaultCfg.LeaderSchedule

	return loadedCfg
}
