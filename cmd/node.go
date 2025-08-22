package cmd

import (
	"context"
	"crypto/ed25519"
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
	"github.com/mezonai/mmn/exception"
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
	dataDir            string
	listenAddr         string
	p2pPort            string
	bootstrapAddresses []string
	grpcAddr           string
	nodeName           string
	// legacy init command
	// database backend
	databaseBackend string
	// Dynamic scheduler flags
	useDynamicScheduler bool
	validatorStake      uint64
	slotsPerEpoch       uint64
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

	// Dynamic scheduler flags
	runCmd.Flags().BoolVar(&useDynamicScheduler, "use-dynamic-scheduler", true, "Use dynamic leader scheduler (Solana-like PoS)")
	runCmd.Flags().Uint64Var(&validatorStake, "validator-stake", 1000000, "Initial validator stake for dynamic scheduler")
	runCmd.Flags().Uint64Var(&slotsPerEpoch, "slots-per-epoch", 432, "Number of slots per epoch for dynamic scheduler")
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
	privKeyPath := filepath.Join(dataDir, "privkey.txt")
	genesisPath := filepath.Join(dataDir, "genesis.yml")
	blockstoreDir := filepath.Join(dataDir, "blockstore")

	// Check if private key exists, fallback to default genesis.yml if genesis.yml not found in data dir
	if _, err := os.Stat(privKeyPath); os.IsNotExist(err) {
		logx.Error("NODE", "Private key file not found at:", privKeyPath)
		logx.Error("NODE", "Please run 'mmn init --data-dir %s' first to initialize the node", dataDir)
		return
	}

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
	txStoreDir := filepath.Join(dataDir, "txstore")
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
	bs, err := initializeBlockstore(blockstoreDir, databaseBackend, ts)
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
	logx.Info("LEDGER", "Loaded ledger state from disk")

	// Initialize PoH components
	_, pohService, recorder, err := initializePoH(cfg, pubKey, genesisPath)
	if err != nil {
		log.Fatalf("Failed to initialize PoH: %v", err)
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

	collector := consensus.NewCollector(3) // TODO: every epoch need have a fixed number

	libP2pClient.SetupCallbacks(ld, privKey, nodeConfig, bs, collector, mp, recorder)

	// Initialize validator
	val, err := initializeValidator(cfg, nodeConfig, pohService, recorder, mp, libP2pClient, bs, ld, collector, privKey, genesisPath)
	if err != nil {
		log.Fatalf("Failed to initialize validator: %v", err)
	}

	// Start services
	startServices(cfg, nodeConfig, libP2pClient, ld, collector, val, bs, mp)

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
func initializeBlockstore(dataDir string, backend string, ts blockstore.TxStore) (blockstore.Store, error) {
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
	return blockstore.CreateStore(config, ts)
}

// initializePoH initializes Proof of History components
func initializePoH(cfg *config.GenesisConfig, pubKey string, genesisPath string) (*poh.Poh, *poh.PohService, *poh.PohRecorder, error) {
	pohCfg, err := config.LoadPohConfig(genesisPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load PoH config: %w", err)
	}

	hashesPerTick := pohCfg.HashesPerTick
	ticksPerSlot := pohCfg.TicksPerSlot
	tickInterval := time.Duration(pohCfg.TickIntervalMs) * time.Millisecond
	pohAutoHashInterval := pohCfg.PohAutoHashInterval

	log.Printf("PoH config: tickInterval=%v, autoHashInterval=%v", tickInterval, pohAutoHashInterval)

	empty_seed := []byte("")
	pohEngine := poh.NewPoh(empty_seed, &hashesPerTick, time.Duration(pohAutoHashInterval)*time.Millisecond)
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

	// Check if dynamic scheduler is enabled
	if useDynamicScheduler || (cfg.DynamicLeaderScheduler != nil && cfg.DynamicLeaderScheduler.Enabled) {
		log.Printf("[DYNAMIC_SCHEDULER]: Using dynamic leader scheduler with PoS features")
		log.Printf("[DYNAMIC_SCHEDULER]: Validator stake: %d, Slots per epoch: %d", validatorStake, slotsPerEpoch)

		// Initialize dynamic components
		dynamicScheduler, dynamicCollector, err := initializeEnhancedComponents(
			nodeConfig.PubKey, validatorStake, slotsPerEpoch, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize enhanced components: %v", err)
		}

		// Create vote account (for now use same as pubkey with suffix)
		voteAccount := nodeConfig.PubKey + "_vote"

		// Create enhanced validator with dynamic PoS features
		enhancedVal := validator.NewEnhancedValidator(
			nodeConfig.PubKey, privKey, recorder, pohService, dynamicScheduler, mp,
			pohCfg.TicksPerSlot, leaderBatchLoopInterval, roleMonitorLoopInterval,
			leaderTimeout, leaderTimeoutLoopInterval, validatorCfg.BatchSize,
			p2pClient, bs, ld, dynamicCollector, voteAccount, validatorStake,
		)
		enhancedVal.Run()

		log.Printf("[DYNAMIC_SCHEDULER]: Enhanced validator started with dynamic PoS features")

		// Convert EnhancedValidator to Validator interface for compatibility
		val := &validator.Validator{
			Pubkey:       enhancedVal.Pubkey,
			PrivKey:      enhancedVal.PrivKey,
			Recorder:     enhancedVal.Recorder,
			Service:      enhancedVal.Service,
			Schedule:     nil, // Not used in dynamic mode
			Mempool:      enhancedVal.Mempool,
			TicksPerSlot: enhancedVal.TicksPerSlot,
		}

		return val, nil
	}

	// Use standard validator with static schedule
	log.Printf("[STATIC_SCHEDULER]: Using static leader schedule")
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

// initializeEnhancedComponents creates dynamic PoS components
func initializeEnhancedComponents(pubKey string, validatorStake, slotsPerEpoch uint64, cfg *config.GenesisConfig) (*poh.DynamicLeaderSchedule, *consensus.DynamicCollector, error) {
	log.Printf("[ENHANCED_COMPONENTS]: Initializing dynamic PoS components")

	// Create validators map from config if available, otherwise use command line params
	validators := make(map[string]*poh.Validator)
	totalStake := uint64(0)

	if cfg.DynamicLeaderScheduler != nil && len(cfg.DynamicLeaderScheduler.Validators) > 0 {
		// Use validators from config
		for _, v := range cfg.DynamicLeaderScheduler.Validators {
			validators[v.Pubkey] = &poh.Validator{
				PubKey:      v.Pubkey,
				Stake:       v.Stake,
				ActiveStake: v.ActiveStake,
				IsActive:    v.IsActive,
			}
			if v.IsActive {
				totalStake += v.ActiveStake
			}
		}
		if cfg.DynamicLeaderScheduler.SlotsPerEpoch > 0 {
			slotsPerEpoch = cfg.DynamicLeaderScheduler.SlotsPerEpoch
		}
	} else {
		// Use command line parameters
		validators[pubKey] = &poh.Validator{
			PubKey:      pubKey,
			Stake:       validatorStake,
			ActiveStake: validatorStake,
			IsActive:    true,
		}
		totalStake = validatorStake
	}

	// Create dynamic leader scheduler
	scheduler := poh.NewDynamicLeaderSchedule(slotsPerEpoch, validators)

	// Scheduler is initialized with epoch 0 by default
	log.Printf("[ENHANCED_COMPONENTS]: Dynamic scheduler initialized with epoch 0")

	// Create dynamic collector with proper parameters
	votingThreshold := 0.67 // Default 2/3 supermajority
	if cfg.DynamicLeaderScheduler != nil && cfg.DynamicLeaderScheduler.VotingThreshold > 0 {
		votingThreshold = cfg.DynamicLeaderScheduler.VotingThreshold
	}

	stakesMap := make(map[string]uint64)
	for pubkey, val := range validators {
		if val.IsActive {
			stakesMap[pubkey] = val.ActiveStake
		}
	}

	dynamicCollector := consensus.NewDynamicCollector(stakesMap, votingThreshold)

	log.Printf("[ENHANCED_COMPONENTS]: Dynamic components initialized successfully")
	log.Printf("[ENHANCED_COMPONENTS]: - Total stake: %d", totalStake)
	log.Printf("[ENHANCED_COMPONENTS]: - Validators: %d", len(validators))
	log.Printf("[ENHANCED_COMPONENTS]: - Slots per epoch: %d", slotsPerEpoch)
	log.Printf("[ENHANCED_COMPONENTS]: - Voting threshold: %.2f", votingThreshold)

	return scheduler, dynamicCollector, nil
}
