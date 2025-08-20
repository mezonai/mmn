package cmd

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log"
	"math/big"
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
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/network"
	"github.com/mezonai/mmn/p2p"
	"github.com/mezonai/mmn/poh"
	"github.com/mezonai/mmn/staking"
	"github.com/mezonai/mmn/validator"

	"github.com/spf13/cobra"
)

const (
	// Storage paths - using absolute paths
	fileBlockDir = "./blockstore/blocks"
	// leveldbBlockDir = "blockstore/leveldb"
)

var (
	dataDir            string
	listenAddr         string
	p2pPort            string
	bootstrapAddresses []string
	grpcAddr           string
	nodeName           string
	// legacy init command
	// database backend
	databaseBackend string
	privateKeyPath  string
	genesisPath     string
)

var runCmd = &cobra.Command{
	Use:   "node",
	Short: "Run the blockchain node",
	Run: func(cmd *cobra.Command, args []string) {
		runNode()
	},
}

func init() {
	rootCmd.AddCommand(runCmd)

	// Run command flags
	runCmd.Flags().StringVar(&dataDir, "data-dir", ".", "Directory containing node data (private key, genesis block, and blockstore)")
	runCmd.Flags().StringVar(&listenAddr, "listen-addr", ":8001", "Listen address for API server :<port>")
	runCmd.Flags().StringVar(&grpcAddr, "grpc-addr", ":9001", "Listen address for Grpc server :<port>")
	runCmd.Flags().StringVar(&p2pPort, "p2p-port", "", "LibP2P listen port (optional, random free port if not specified)")
	runCmd.Flags().StringArrayVar(&bootstrapAddresses, "bootstrap-addresses", []string{}, "List of bootstrap peer multiaddresses")
	runCmd.Flags().StringVar(&nodeName, "node-name", "node1", "Node name for loading genesis configuration")
	runCmd.Flags().StringVar(&databaseBackend, "database", "leveldb", "Database backend (leveldb or rocksdb)")
	runCmd.Flags().StringVar(&privateKeyPath, "privkey-path", "privkey.txt", "Path to the private key file")
	runCmd.Flags().StringVar(&genesisPath, "genesis-path", "genesis.yml", "Path to the genesis configuration file")
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

	// Construct paths from data directory
	privKeyPath := filepath.Join(dataDir, privateKeyPath)
	genesisPath := filepath.Join(dataDir, genesisPath)

	// Extract port number from grpcAddr (format ":9001" -> "9001")
	port := grpcAddr[1:] // Remove ':' prefix
	if port == "" {
		port = "default"
	}
	blockstoreDir := filepath.Join(dataDir, "blockstore", fmt.Sprintf("blocks_%s", port))

	// Check if private key exists, fallback to default genesis.yml if genesis.yml not found in data dir
	if _, err := os.Stat(privKeyPath); os.IsNotExist(err) {
		logx.Error("NODE", "Private key file not found at:", privKeyPath)
		logx.Error("NODE", "Please run 'mmn init --data-dir %s' first to initialize the node", dataDir)
		return
	}

	// Create node-specific directory to avoid database conflicts
	// nodeSpecificDir := fmt.Sprintf("%s_%s", leveldbBlockDir, grpcAddr[1:]) // Remove ':' from port
	// if nodeSpecificDir == leveldbBlockDir+"_" {
	// 	nodeSpecificDir = leveldbBlockDir + "_default"
	// }
	// nodeSpecificDir := fmt.Sprintf("%s_%s", filepath.Join(dataDir, fileBlockDir), grpcAddr[1:])

	// Create absolute path for storage
	// absLeveldbBlockDir := filepath.Join(currentDir, nodeSpecificDir)

	// Check if genesis.yml exists in data dir, fallback to config/genesis.yml
	if _, err := os.Stat(genesisPath); os.IsNotExist(err) {
		logx.Info("NODE", "Genesis file not found in data directory, using default config/genesis.yml")
		genesisPath = "config/genesis.yml"
	}

	// Create blockstore directory if it doesn't exist
	if err := os.MkdirAll(blockstoreDir, 0755); err != nil {
		logx.Error("NODE", "Failed to create blockstore directory:", err.Error())
		return
	}

	pubKey, err := config.LoadPubKeyFromPriv(privKeyPath)
	if err != nil {
		logx.Error("NODE", "Failed to load public key:", err.Error())
		return
	}

	// Initialize tx store
	// TODO: avoid duplication with cmd.initializeNode
	txStoreDir := filepath.Join(initDataDir, "txstore")
	if err := os.MkdirAll(txStoreDir, 0755); err != nil {
		logx.Error("INIT", "Failed to create txstore directory:", err.Error())
		return
	}
	ts, err := initializeTxStore(txStoreDir, initDatabase)
	if err != nil {
		logx.Error("INIT", "Failed to create txstore directory:", err.Error())
		return
	}
	defer ts.Close()

	// Initialize blockstore with data directory
	bs, err := initializeBlockstore(pubKey, blockstoreDir, databaseBackend, ts)
	if err != nil {
		logx.Error("NODE", "Failed to initialize blockstore:", err.Error())
		return
	}

	// Handle optional p2p-port: use random free port if not specified
	if p2pPort == "" {
		p2pPort, err = getRandomFreePort()
		if err != nil {
			logx.Error("NODE", "Failed to get random free port:", err.Error())
			return
		}
		logx.Info("NODE", "Using random P2P port:", p2pPort)
	}

	// Load genesis configuration from file
	cfg, err := loadConfiguration(genesisPath)
	if err != nil {
		logx.Error("NODE", "Failed to load genesis configuration:", err.Error())
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

	ld := ledger.NewLedger(ts)

	// Load ledger state from disk (includes alloc account from genesis)
	if err := ld.LoadLedger(); err != nil {
		log.Fatalf("Failed to load ledger state: %v", err)
	}

	// Check if genesis accounts exist, if not create them
	genesisAccountExists := false
	for _, addr := range cfg.Alloc.Addresses {
		if ld.AccountExists(addr.Address) {
			genesisAccountExists = true
			break
		}
	}

	if !genesisAccountExists && len(cfg.Alloc.Addresses) > 0 {
		logx.Info("LEDGER", "Genesis accounts not found, creating them from genesis config")
		err := ld.CreateAccountsFromGenesis(cfg.Alloc.Addresses)
		if err != nil {
			log.Fatalf("Failed to create genesis accounts: %v", err)
		}

		// Save ledger snapshot to persist genesis accounts
		if err := ld.SaveSnapshot("ledger/snapshot.gob"); err != nil {
			log.Fatalf("Failed to save ledger snapshot: %v", err)
		}
		logx.Info("LEDGER", "Genesis accounts created and snapshot saved")
	}

	logx.Info("LEDGER", "Loaded ledger state from disk")

	// Initialize PoH components
	_, pohService, recorder, err := initializePoH(cfg, pubKey, genesisPath)
	if err != nil {
		log.Fatalf("Failed to initialize PoH: %v", err)
	}

	// Load PoH config for later use
	_, err = config.LoadPohConfig(genesisPath)
	if err != nil {
		log.Fatalf("Failed to load PoH config: %v", err)
	}

	// Load private key
	privKey, err := config.LoadEd25519PrivKey(privKeyPath)
	if err != nil {
		log.Fatalf("load private key: %v", err)
	}

	// Initialize network
	libP2pClient, err := initializeNetwork(nodeConfig, bs, privKey)
	if err != nil {
		log.Fatalf("Failed to initialize network: %v", err)
	}

	// Initialize mempool
	mp, err := initializeMempool(libP2pClient, ld, genesisPath)
	if err != nil {
		log.Fatalf("Failed to initialize mempool: %v", err)
	}

	// DYNAMIC COLLECTOR: Start with minimal configuration, let it auto-adapt
	// The collector will automatically discover and adjust to actual validator count
	initialValidators := 1 // Start with self only

	// Consider genesis validators for minimum setup
	genesisValidators := len(cfg.GenesisValidators)
	if genesisValidators > initialValidators {
		initialValidators = genesisValidators
	}

	log.Printf("INFO: Initializing collector for %d initial validators (will auto-adapt based on actual voting)", initialValidators)
	collector := consensus.NewCollector(initialValidators) // DYNAMIC with auto-adaptation!

	libP2pClient.SetupCallbacks(ld, privKey, nodeConfig, bs, collector, mp)

	// INITIALIZE STAKE MANAGER for dynamic PoS scheduling
	var stakeManager *staking.StakeManager
	var dynamicSchedule *poh.LeaderSchedule

	// DEBUG: Check staking configuration
	log.Printf("DEBUG: cfg.Staking.Enabled = %v", cfg.Staking.Enabled)
	log.Printf("DEBUG: len(cfg.GenesisValidators) = %d", len(cfg.GenesisValidators))

	if cfg.Staking.Enabled {
		log.Printf("INFO: Initializing StakeManager with dynamic validator registration")

		stakeManager, err = initializeStakeManager(cfg, recorder, ld, mp, bs, libP2pClient, collector, genesisPath)
		if err != nil {
			log.Fatalf("Failed to initialize stake manager: %v", err)
		}

		// START STAKE MANAGER to generate initial leader schedule
		err = stakeManager.Start(ctx)
		if err != nil {
			log.Fatalf("Failed to start stake manager: %v", err)
		}

		// GET DYNAMIC LEADER SCHEDULE - replaces hardcode!
		dynamicSchedule = stakeManager.GetCurrentLeaderSchedule()
		if dynamicSchedule == nil {
			log.Fatalf("Failed to get dynamic leader schedule from StakeManager")
		}

		log.Printf("INFO: Using dynamic PoS leader schedule with stake-based allocation")

		// UPDATE EXISTING PoH RECORDER with dynamic schedule (DON'T CREATE NEW INSTANCE!)
		recorder.UpdateLeaderSchedule(dynamicSchedule)
	} else {
		dynamicSchedule = nil
	}

	// Initialize validator (with dynamic or legacy schedule)
	val, err := initializeValidatorWithSchedule(cfg, nodeConfig, pohService, recorder, dynamicSchedule, mp, libP2pClient, bs, ld, collector, privKey, genesisPath)
	if err != nil {
		log.Fatalf("Failed to initialize validator: %v", err)
	}

	// Start services
	startServices(cfg, nodeConfig, libP2pClient, ld, collector, val, bs, mp)

	// Start periodic collector cleanup to prevent memory leaks
	exception.SafeGoWithPanic("Collector cleanup", func() {
		ticker := time.NewTicker(30 * time.Second) // cleanup every 30s
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				currentSlot := val.GetCurrentSlot()         // You may need to expose this method
				collector.CleanupOldVotes(currentSlot, 100) // keep last 100 slots
			case <-ctx.Done():
				return
			}
		}
	})

	exception.SafeGoWithPanic("Shutting down", func() {
		<-sigCh
		log.Println("Shutting down node...")
		// for now just shutdown p2p network
		libP2pClient.Close()
		cancel()
	})

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

// initializeTxStore initializes the tx storage backend
func initializeTxStore(dataDir string, backend string) (blockstore.TxStore, error) {
	dbProvider, err := blockstore.CreateDBProvider(blockstore.DBVendor(backend), blockstore.DBOptions{
		Directory: dataDir,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create db provider: %w", err)
	}
	return blockstore.NewGenericTxStore(dbProvider)
}

// initializeBlockstore initializes the block storage backend using the factory pattern
func initializeBlockstore(pubKey string, dataDir string, backend string, ts blockstore.TxStore) (blockstore.Store, error) {
	seed := []byte(pubKey)

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
	return blockstore.CreateStore(config, seed, ts)
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
	pohAutoHashInterval := pohCfg.PohAutoHashInterval

	log.Printf("PoH config: tickInterval=%v, autoHashInterval=%v", tickInterval, pohAutoHashInterval)

	pohEngine := poh.NewPoh(seed, &hashesPerTick, time.Duration(pohAutoHashInterval))
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
func initializeMempool(p2pClient *p2p.Libp2pNetwork, ld *ledger.Ledger, genesisPath string) (*mempool.Mempool, error) {
	mempoolCfg, err := config.LoadMempoolConfig(genesisPath)
	if err != nil {
		return nil, fmt.Errorf("load mempool config: %w", err)
	}

	mp := mempool.NewMempool(mempoolCfg.MaxTxs, p2pClient, ld)
	return mp, nil
}

// initializeStakeManager initializes the stake manager with dynamic validators
func initializeStakeManager(
	cfg *config.GenesisConfig,
	pohRecorder *poh.PohRecorder,
	ledger *ledger.Ledger,
	mempool *mempool.Mempool,
	blockStore blockstore.Store,
	p2pNetwork *p2p.Libp2pNetwork,
	collector *consensus.Collector,
	genesisPath string,
) (*staking.StakeManager, error) {

	// Convert staking config
	stakingManagerConfig, err := ConvertStakingConfig(&cfg.Staking)
	if err != nil {
		return nil, fmt.Errorf("invalid staking config: %w", err)
	}

	// Create stake manager
	stakeManager := staking.NewStakeManager(
		stakingManagerConfig,
		pohRecorder,
		ledger,
		mempool,
		blockStore,
		p2pNetwork,
		collector,
	)

	// AUTO-REGISTER this node as a validator with default stake
	defaultStakeAmount := new(big.Int).SetUint64(10000000) // 10M tokens default
	currentNodePubkey := p2pNetwork.GetSelfPublicKey()

	log.Printf("Auto-registering current node as validator...")
	err = stakeManager.RegisterGenesisValidator(currentNodePubkey, defaultStakeAmount)
	if err != nil {
		return nil, fmt.Errorf("failed to auto-register node as validator %s: %w", currentNodePubkey, err)
	}

	log.Printf("Auto-registered node %s as validator with stake %s (no commission)",
		currentNodePubkey, defaultStakeAmount.String())

	// Also register any hardcode genesis validators if they exist (backward compatibility)
	if len(cfg.GenesisValidators) > 0 {
		log.Printf("Additionally registering %d configured genesis validators...", len(cfg.GenesisValidators))

		for i, gv := range cfg.GenesisValidators {
			// Parse stake amount
			stakeAmount, ok := new(big.Int).SetString(gv.StakeAmount, 10)
			if !ok {
				return nil, fmt.Errorf("invalid stake amount for validator %d: %s", i, gv.StakeAmount)
			}

			// Register genesis validator (activates immediately)
			err := stakeManager.RegisterGenesisValidator(gv.Pubkey, stakeAmount)
			if err != nil {
				return nil, fmt.Errorf("failed to register genesis validator %s: %w", gv.Pubkey, err)
			}

			log.Printf("Registered validator %s with stake %s (no commission)",
				gv.Pubkey, gv.StakeAmount)
		}
	}

	return stakeManager, nil
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

// initializeValidatorWithSchedule initializes the validator with optional dynamic schedule
func initializeValidatorWithSchedule(cfg *config.GenesisConfig, nodeConfig config.NodeConfig, pohService *poh.PohService, recorder *poh.PohRecorder,
	dynamicSchedule *poh.LeaderSchedule, mp *mempool.Mempool, p2pClient *p2p.Libp2pNetwork, bs blockstore.Store, ld *ledger.Ledger,
	collector *consensus.Collector, privKey ed25519.PrivateKey, genesisPath string) (*validator.Validator, error) {

	validatorCfg, err := config.LoadValidatorConfig(genesisPath)
	if err != nil {
		return nil, fmt.Errorf("load validator config: %w", err)
	}

	// Calculate intervals
	pohCfg, _ := config.LoadPohConfig(genesisPath)
	tickInterval := time.Duration(pohCfg.TickIntervalMs) * time.Millisecond
	leaderBatchLoopInterval := tickInterval / 2
	roleMonitorLoopInterval := tickInterval
	leaderTimeout := time.Duration(validatorCfg.LeaderTimeout) * time.Millisecond
	leaderTimeoutLoopInterval := time.Duration(validatorCfg.LeaderTimeoutLoopInterval) * time.Millisecond

	log.Printf("Validator config: batchLoopInterval=%v, monitorLoopInterval=%v",
		leaderBatchLoopInterval, roleMonitorLoopInterval)

	// Choose schedule: dynamic PoS or legacy hardcode
	var schedule *poh.LeaderSchedule
	if dynamicSchedule != nil {
		schedule = dynamicSchedule
		log.Printf("INFO: Validator using dynamic PoS leader schedule")
	} else {
		schedule = config.ConvertLeaderSchedule(cfg.LeaderSchedule)
		log.Printf("INFO: Validator using legacy hardcode leader schedule")
	}

	val := validator.NewValidator(
		nodeConfig.PubKey, privKey, recorder, pohService,
		schedule, mp, pohCfg.TicksPerSlot,
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
