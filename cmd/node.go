package cmd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
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
	leveldbBlockDir = "blockstore/leveldb"
)

var (
	privKeyPath        string
	listenAddr         string
	p2pPort            string
	bootstrapAddresses []string
	grpcAddr           string
	nodeName           string
	genesisPath        string
	// init command
	outputDir string
	// database backend
	databaseBackend string
)

var runCmd = &cobra.Command{
	Use:   "node",
	Short: "Run the blockchain node",
	Run: func(cmd *cobra.Command, args []string) {
		runNode()
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize node by generating a private key",
	Run: func(cmd *cobra.Command, args []string) {
		generatePrivateKey()
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.AddCommand(initCmd)
	runCmd.Flags().StringVar(&privKeyPath, "privkey-path", "", "Path to private key file")
	runCmd.Flags().StringVar(&listenAddr, "listen-addr", ":8001", "Listen address for API server :<port>")
	runCmd.Flags().StringVar(&grpcAddr, "grpc-addr", ":9001", "Listen address for Grpc server :<port>")
	runCmd.Flags().StringVar(&p2pPort, "p2p-port", "", "LibP2P listen port (optional, random free port if not specified)")
	runCmd.Flags().StringArrayVar(&bootstrapAddresses, "bootstrap-addresses", []string{}, "List of bootstrap peer multiaddresses")
	runCmd.Flags().StringVar(&nodeName, "node-name", "node1", "Node name for loading genesis configuration")
	runCmd.Flags().StringVar(&genesisPath, "genesis-path", "config/genesis.yml", "Path to genesis configuration file")
	runCmd.Flags().StringVar(&databaseBackend, "database", "leveldb", "Database backend (leveldb or rocksdb)")

	// Init command flags
	initCmd.Flags().StringVar(&outputDir, "output-dir", ".", "Output directory for the generated private key file")
}

// getRandomFreePort returns a random free port
func getRandomFreePort() (string, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	addr := listener.Addr().(*net.TCPAddr)
	return strconv.Itoa(addr.Port), nil
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
	absLeveldbBlockDir := filepath.Join(currentDir, leveldbBlockDir)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(absLeveldbBlockDir, 0755); err != nil {
		logx.Warn("Warning: Failed to create directory %s: %v", absLeveldbBlockDir, err)
	}

	pubKey, err := config.LoadPubKeyFromPriv(privKeyPath)
	if err != nil {
		logx.Error("Public Key", err.Error())
	}
	// Initialize components with absolute paths
	bs, err := initializeBlockstore(pubKey, absLeveldbBlockDir, databaseBackend)
	if err != nil {
		logx.Warn("Failed to initialize blockstore: %v", err)
	}

	// Handle optional p2p-port: use random free port if not specified
	if p2pPort == "" {
		p2pPort, err = getRandomFreePort()
		if err != nil {
			logx.Error("Failed to get random free port: %v", err)
			return
		}
		logx.Info("Using random P2P port: %s", p2pPort)
	}

	// Load genesis configuration from file
	cfg, err := loadConfiguration(genesisPath)
	if err != nil {
		logx.Error("Failed to load genesis configuration: %v", err)
		return
	}

	// Create node configuration from command-line arguments
	nodeConfig := config.NodeConfig{
		PubKey:             pubKey,
		PrivKeyPath:        privKeyPath,
		Libp2pAddr:         fmt.Sprintf("/ip4/0.0.0.0/tcp/%s", p2pPort),
		ListenAddr:         listenAddr,
		GRPCAddr:           grpcAddr,
		BootStrapAddresses: bootstrapAddresses,
	}

	if privKeyPath == "" {
		logx.Error("Private Key Empty")
		return
	}

	ld := ledger.NewLedger(cfg.Faucet.Address)

	// Initialize PoH components
	_, pohService, recorder, err := initializePoH(cfg, pubKey, genesisPath)
	if err != nil {
		log.Fatalf("Failed to initialize PoH: %v", err)
	}

	// Load private key
	privKey, err := config.LoadEd25519PrivKey(privKeyPath)
	if err != nil {
		log.Fatalf("load private key: %w", err)
	}

	// Initialize network
	libP2pClient, err := initializeNetwork(nodeConfig, bs, privKey)
	if err != nil {
		log.Fatalf("Failed to initialize network: %v", err)
	}

	// Initialize mempool
	mp, err := initializeMempool(libP2pClient, genesisPath)
	if err != nil {
		log.Fatalf("Failed to initialize mempool: %v", err)
	}

	collector := consensus.NewCollector(libP2pClient.GetPeersConnected() + 1)

	libP2pClient.SetupCallbacks(ld, privKey, nodeConfig, bs, collector, mp)

	// Initialize validator
	val, err := initializeValidator(cfg, nodeConfig, pohService, recorder, mp, libP2pClient, bs, ld, collector, privKey, genesisPath)
	if err != nil {
		log.Fatalf("Failed to initialize validator: %v", err)
	}

	// Start services
	startServices(cfg, nodeConfig, libP2pClient, ld, collector, val, bs, mp)

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
func loadConfiguration(genesisPath string) (*config.GenesisConfig, error) {
	cfg, err := config.LoadGenesisConfig(genesisPath)
	if err != nil {
		return nil, fmt.Errorf("load genesis config: %w", err)
	}
	return cfg, nil
}

// initializeBlockstore initializes the block storage backend using the factory pattern
func initializeBlockstore(pubKey string, dataDir string, backend string) (blockstore.Store, error) {
	seed := []byte(pubKey)
	if len(seed) > 32 {
		seed = seed[:32]
	} else if len(seed) < 32 {
		padded := make([]byte, 32)
		copy(padded, seed)
		seed = padded
	}

	// Create store configuration with StoreType
	storeType := blockstore.StoreType(backend)
	config := &blockstore.StoreConfig{
		Type:      storeType,
		Directory: dataDir,
	}

	// Validate the configuration (this will check if the backend is supported)
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid blockstore configuration: %w", err)
	}

	// Use the factory pattern to create the store
	return blockstore.CreateStore(config, seed)
}

// initializePoH initializes Proof of History components
func initializePoH(cfg *config.GenesisConfig, pubKey string, genesisPath string) (*poh.Poh, *poh.PohService, *poh.PohRecorder, error) {
	pohCfg, err := config.LoadPohConfig(genesisPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load PoH config: %w", err)
	}

	seed := []byte(pubKey)
	hashesPerTick := pohCfg.HashesPerTick
	ticksPerSlot := pohCfg.TicksPerSlot
	tickInterval := time.Duration(pohCfg.TickIntervalMs) * time.Millisecond
	pohAutoHashInterval := tickInterval / 5

	log.Printf("PoH config: tickInterval=%v, autoHashInterval=%v", tickInterval, pohAutoHashInterval)

	pohEngine := poh.NewPoh(seed, &hashesPerTick, pohAutoHashInterval)
	pohEngine.Run()

	pohSchedule := config.ConvertLeaderSchedule(cfg.LeaderSchedule)
	recorder := poh.NewPohRecorder(pohEngine, ticksPerSlot, pubKey, pohSchedule)

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
func initializeMempool(p2pClient *p2p.Libp2pNetwork, genesisPath string) (*mempool.Mempool, error) {
	mempoolCfg, err := config.LoadMempoolConfig(genesisPath)
	if err != nil {
		return nil, fmt.Errorf("load mempool config: %w", err)
	}

	mp := mempool.NewMempool(mempoolCfg.MaxTxs, p2pClient)
	return mp, nil
}

// initializeValidator initializes the validator
func initializeValidator(cfg *config.GenesisConfig, nodeConfig config.NodeConfig, pohService *poh.PohService, recorder *poh.PohRecorder,
	mp *mempool.Mempool, p2pClient *p2p.Libp2pNetwork, bs blockstore.Store, ld *ledger.Ledger,
	collector *consensus.Collector, privKey ed25519.PrivateKey, genesisPath string) (*validator.Validator, error) {

	validatorCfg, err := config.LoadValidatorConfig(genesisPath)
	if err != nil {
		return nil, fmt.Errorf("load validator config: %w", err)
	}

	// Calculate intervals
	pohCfg, _ := config.LoadPohConfig(genesisPath) // Already loaded in initializePoH
	tickInterval := time.Duration(pohCfg.TickIntervalMs) * time.Millisecond
	leaderBatchLoopInterval := tickInterval / 2
	roleMonitorLoopInterval := tickInterval
	leaderTimeout := time.Duration(validatorCfg.LeaderTimeout) * time.Millisecond
	leaderTimeoutLoopInterval := time.Duration(validatorCfg.LeaderTimeoutLoopInterval) * time.Millisecond

	log.Printf("Validator config: batchLoopInterval=%v, monitorLoopInterval=%v",
		leaderBatchLoopInterval, roleMonitorLoopInterval)

	val := validator.NewValidator(
		nodeConfig.PubKey, privKey, recorder, pohService,
		config.ConvertLeaderSchedule(cfg.LeaderSchedule), mp, pohCfg.TicksPerSlot,
		leaderBatchLoopInterval, roleMonitorLoopInterval, leaderTimeout,
		leaderTimeoutLoopInterval, validatorCfg.BatchSize, p2pClient, bs, ld, collector,
	)
	val.Run()

	return val, nil
}

// startServices starts all network and API services
func startServices(cfg *config.GenesisConfig, nodeConfig config.NodeConfig, p2pClient *p2p.Libp2pNetwork, ld *ledger.Ledger, collector *consensus.Collector,
	val *validator.Validator, bs blockstore.Store, mp *mempool.Mempool) {

	// Load private key for gRPC server
	privKey, err := config.LoadEd25519PrivKey(nodeConfig.PrivKeyPath)
	if err != nil {
		log.Fatalf("Failed to load private key for gRPC server: %v", err)
	}

	// Start gRPC server
	grpcSrv := network.NewGRPCServer(
		nodeConfig.GRPCAddr,
		map[string]ed25519.PublicKey{},
		fileBlockDir,
		ld,
		collector,
		nodeConfig.PubKey,
		privKey,
		val,
		bs,
		mp,
	)
	_ = grpcSrv // Keep server running

	// Start API server on a different port
	apiSrv := api.NewAPIServer(mp, ld, nodeConfig.ListenAddr)
	apiSrv.Start()
}

// generatePrivateKey generates a new Ed25519 seed and saves it to privkey.txt
func generatePrivateKey() {
	// Generate 32-byte Ed25519 seed
	seed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(seed)
	if err != nil {
		log.Fatalf("Failed to generate Ed25519 seed: %v", err)
	}

	// Convert seed to hex string
	seedHex := hex.EncodeToString(seed)

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Create full path for the private key file
	filename := filepath.Join(outputDir, "privkey.txt")
	err = os.WriteFile(filename, []byte(seedHex), 0600)
	if err != nil {
		log.Fatalf("Failed to write private key to file: %v", err)
	}

	logx.Info("KEYGEN", "Successfully generated Ed25519 seed and saved to", filename)
	logx.Info("KEYGEN", "Seed length:", len(seed), "bytes")
}
