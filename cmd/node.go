package cmd

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/mezonai/mmn/monitoring"
	"github.com/mezonai/mmn/security/abuse"
	"github.com/mezonai/mmn/zkverify"

	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/jsonrpc"
	"github.com/mezonai/mmn/network"
	"github.com/mezonai/mmn/security/ratelimit"
	"github.com/mezonai/mmn/service"
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/transaction"

	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/p2p"
	"github.com/mezonai/mmn/poh"
	"github.com/mezonai/mmn/validator"

	"github.com/spf13/cobra"
)

const (
	// Storage paths - using absolute paths
	fileBlockDir    = "./blockstore/blocks"
	leveldbBlockDir = "blockstore/leveldb"
	LISTEN_MODE     = "listen"
	FULL_MODE       = "full"
)

var (
	dataDir            	string
	listenAddr         	string
	jsonrpcAddr        	string
	p2pPort            	string
	bootstrapAddresses 	[]string
	grpcAddr           	string
	nodeName           	string
	mode               	string
	// legacy init command
	// database backend
	databaseBackend 	string
	rateLimit 			bool
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
	runCmd.Flags().StringVar(&jsonrpcAddr, "jsonrpc-addr", ":8080", "Listen address for JSON-RPC server :<port>")
	runCmd.Flags().StringVar(&grpcAddr, "grpc-addr", ":9001", "Listen address for Grpc server :<port>")
	runCmd.Flags().StringVar(&p2pPort, "p2p-port", "", "LibP2P listen port (optional, random free port if not specified)")
	runCmd.Flags().StringArrayVar(&bootstrapAddresses, "bootstrap-addresses", []string{}, "List of bootstrap peer multiaddresses")
	runCmd.Flags().StringVar(&nodeName, "node-name", "node1", "Node name for loading genesis configuration")
	runCmd.Flags().StringVar(&databaseBackend, "database", "leveldb", "Database backend (leveldb or rocksdb)")
	runCmd.Flags().StringVar(&mode, "mode", FULL_MODE, "Node mode: full or listen")
	runCmd.Flags().BoolVar(&rateLimit, "rate-limit", true, "enable rate limit for json-rpc and grpc")

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
	initializeFileLogger()
	monitoring.InitMetrics()

	logx.Info("NODE", "Running node", "with", "rate limit", rateLimit)

	// Handle Docker stop or Ctrl+C
	ctx, cancel := context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Construct paths from data directory
	privKeyPath := filepath.Join(dataDir, "privkey.txt")
	genesisPath := filepath.Join(dataDir, "genesis.yml")
	zkVerifyPath := filepath.Join(dataDir, "verifying_key.b64")
	dbStoreDir := filepath.Join(dataDir, "store")

	// Check if private key exists, fallback to default genesis.yml if genesis.yml not found in data dir
	if _, err := os.Stat(privKeyPath); os.IsNotExist(err) {
		logx.Error("NODE", "Private key file not found at:", privKeyPath)
		return
	}

	// Check if genesis.yml exists in data dir, fallback to config/genesis.yml
	if _, err := os.Stat(genesisPath); os.IsNotExist(err) {
		logx.Info("NODE", "Genesis file not found in data directory, using default config/genesis.yml")
		genesisPath = "config/genesis.yml"
	}

	// Check if zk verify key exists in data dir, fallback to config/verifying_key.b64
	if _, err := os.Stat(zkVerifyPath); os.IsNotExist(err) {
		logx.Info("NODE", "Zk verify key file not found in data directory, using default config/verifying_key.b64")
		zkVerifyPath = "config/verifying_key.b64"
	}

	// Create blockstore directory if it doesn't exist
	if err := os.MkdirAll(dbStoreDir, 0755); err != nil {
		logx.Error("NODE", "Failed to create blockstore directory:", err.Error())
		return
	}

	pubKey, err := config.LoadPubKeyFromPriv(privKeyPath)
	if err != nil {
		logx.Error("NODE", "Failed to load public key:", err.Error())
		return
	}

	// --- Event Bus ---
	eventBus := events.NewEventBus()

	// --- Event Router ---
	eventRouter := events.NewEventRouter(eventBus)

	// Initialize db store inside directory
	as, ts, tms, bs, err := initializeDBStore(dbStoreDir, databaseBackend, eventRouter)
	if err != nil {
		logx.Error("NODE", "Failed to initialize blockstore:", err.Error())
		return
	}
	defer bs.MustClose()
	defer ts.MustClose()
	defer tms.MustClose()
	defer as.MustClose()

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
		JSONRPCAddr:        jsonrpcAddr,
		GRPCAddr:           grpcAddr,
		BootStrapAddresses: bootstrapAddresses,
		Mode:               mode,
	}

	txTracker := transaction.NewTransactionTracker()

	ld := ledger.NewLedger(ts, tms, as, eventRouter, txTracker)

	// Initialize PoH components
	latestSlot := bs.GetLatestFinalizedSlot()
	_, pohService, recorder, err := initializePoH(cfg, pubKey, genesisPath, latestSlot)
	if err != nil {
		log.Fatalf("Failed to initialize PoH: %v", err)
	}

	// Load private key
	privKey, err := config.LoadEd25519PrivKey(privKeyPath)
	if err != nil {
		log.Fatalf("load private key: %v", err)
	}

	// Initialize network
	libP2pClient, err := initializeNetwork(nodeConfig, bs, ts, privKey, &cfg.Poh, mode)
	if err != nil {
		log.Fatalf("Failed to initialize network: %v", err)
	}

	// Initialize zk verify
	zkVerify := zkverify.NewZkVerify(zkVerifyPath)

	// Initialize mempool
	mp, err := initializeMempool(libP2pClient, ld, genesisPath, eventRouter, txTracker, zkVerify)
	if err != nil {
		log.Fatalf("Failed to initialize mempool: %v", err)
	}

	collector := consensus.NewCollector(len(cfg.LeaderSchedule))

	libP2pClient.SetupCallbacks(ld, privKey, nodeConfig, bs, collector, mp, recorder)

	// Initialize validator
	val, err := initializeValidator(cfg, nodeConfig, pohService, recorder, mp, libP2pClient, bs, privKey, genesisPath, ld, collector)
	if err != nil {
		log.Fatalf("Failed to initialize validator: %v", err)
	}

	// In listen mode, do not start PoH or Validator
	if nodeConfig.Mode != LISTEN_MODE {
		libP2pClient.OnStartPoh = func() { pohService.Start() }
		libP2pClient.OnStartValidator = func() { val.Run() }
	}
	libP2pClient.SetupPubSubSyncTopics(ctx)

	startServices(nodeConfig, ld, collector, val, bs, mp, eventRouter, txTracker)

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

// initializeDBStore initializes the block storage backend using the factory pattern
func initializeDBStore(dataDir string, backend string, eventRouter *events.EventRouter) (store.AccountStore, store.TxStore, store.TxMetaStore, store.BlockStore, error) {
	// Create data folder if not exist
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logx.Error("INIT", "Failed to create db store directory:", err.Error())
		return nil, nil, nil, nil, err
	}

	// Create store configuration with StoreType
	storeType := store.StoreType(backend)
	storeCfg := &store.StoreConfig{
		Type:      storeType,
		Directory: dataDir,
	}

	// Validate the configuration (this will check if the backend is supported)
	if err := storeCfg.Validate(); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("invalid blockstore configuration: %w", err)
	}

	// Use the factory pattern to create the store
	return store.CreateStore(storeCfg, eventRouter)
}

// initializePoH initializes Proof of History components
func initializePoH(cfg *config.GenesisConfig, pubKey string, genesisPath string, latestSlot uint64) (*poh.Poh, *poh.PohService, *poh.PohRecorder, error) {
	pohCfg, err := config.LoadPohConfig(genesisPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load PoH config: %w", err)
	}

	hashesPerTick := pohCfg.HashesPerTick
	ticksPerSlot := pohCfg.TicksPerSlot
	tickInterval := time.Duration(pohCfg.TickIntervalMs) * time.Millisecond
	pohAutoHashInterval := tickInterval / 10

	log.Printf("PoH config: tickInterval=%v, autoHashInterval=%v", tickInterval, pohAutoHashInterval)

	empty_seed := []byte("")
	pohEngine := poh.NewPoh(empty_seed, &hashesPerTick, pohAutoHashInterval)
	pohEngine.Run()

	pohSchedule := config.ConvertLeaderSchedule(cfg.LeaderSchedule)
	recorder := poh.NewPohRecorder(pohEngine, ticksPerSlot, pubKey, pohSchedule, latestSlot)

	pohService := poh.NewPohService(recorder, tickInterval)

	return pohEngine, pohService, recorder, nil
}

// initializeNetwork initializes network components
func initializeNetwork(self config.NodeConfig, bs store.BlockStore, ts store.TxStore, privKey ed25519.PrivateKey, pohCfg *config.PohConfig, mode string) (*p2p.Libp2pNetwork, error) {
	// Prepare peer addresses (excluding self)
	libp2pNetwork, err := p2p.NewNetWork(
		self.PubKey,
		privKey,
		self.Libp2pAddr,
		self.BootStrapAddresses,
		bs,
		ts,
		pohCfg,
		mode == LISTEN_MODE,
	)

	return libp2pNetwork, err
}

// initializeMempool initializes the mempool
func initializeMempool(p2pClient *p2p.Libp2pNetwork, ld *ledger.Ledger, genesisPath string,
	eventRouter *events.EventRouter, txTracker interfaces.TransactionTrackerInterface, zkVerify *zkverify.ZkVerify) (*mempool.Mempool, error) {
	mempoolCfg, err := config.LoadMempoolConfig(genesisPath)
	if err != nil {
		return nil, fmt.Errorf("load mempool config: %w", err)
	}

	mp := mempool.NewMempool(mempoolCfg.MaxTxs, p2pClient, ld, eventRouter, txTracker, zkVerify)
	return mp, nil
}

// initializeValidator initializes the validator
func initializeValidator(cfg *config.GenesisConfig, nodeConfig config.NodeConfig, pohService *poh.PohService, recorder *poh.PohRecorder,
	mp *mempool.Mempool, p2pClient *p2p.Libp2pNetwork, bs store.BlockStore, privKey ed25519.PrivateKey, genesisPath string, ld *ledger.Ledger, collector *consensus.Collector) (*validator.Validator, error) {

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
		config.ConvertLeaderSchedule(cfg.LeaderSchedule), mp,
		leaderBatchLoopInterval, roleMonitorLoopInterval, leaderTimeout,
		leaderTimeoutLoopInterval, validatorCfg.BatchSize, p2pClient, bs,
		ld, collector,
	)

	// Cache leader schedule inside p2p for local leader checks
	p2pClient.ApplyLeaderSchedule(val.Schedule)

	return val, nil
}

// startServices starts all network and API services
func startServices(nodeConfig config.NodeConfig, ld *ledger.Ledger, collector *consensus.Collector,
	val *validator.Validator, bs store.BlockStore, mp *mempool.Mempool, eventRouter *events.EventRouter, txTracker interfaces.TransactionTrackerInterface) {

	// Load private key for gRPC server
	privKey, err := config.LoadEd25519PrivKey(nodeConfig.PrivKeyPath)
	if err != nil {
		log.Fatalf("Failed to load private key for gRPC server: %v", err)
	}

	abuseConfig := abuse.DefaultAbuseConfig()
	abuseDetector := abuse.NewAbuseDetector(abuseConfig)

	rateLimiterConfig := ratelimit.DefaultGlobalConfig()
	rateLimiter := ratelimit.NewGlobalRateLimiterWithAbuseDetector(rateLimiterConfig, abuseDetector)
	defer rateLimiter.Stop()

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
		eventRouter,
		txTracker,
		rateLimiter,
		rateLimit,
	)
	_ = grpcSrv // Keep server running

	// Start JSON-RPC server on dedicated JSON-RPC address using shared services with protection
	txSvc := service.NewTxService(ld, mp, bs, txTracker)
	acctSvc := service.NewAccountService(ld, mp, txTracker)
	rpcSrv := jsonrpc.NewServer(nodeConfig.JSONRPCAddr, txSvc, acctSvc, rateLimiter, rateLimit)

	// Apply CORS from environment variables via jsonrpc helper (default denies all)
	if corsCfg, ok := jsonrpc.CORSFromEnv(); ok {
		rpcSrv.SetCORSConfig(corsCfg)
	}

	rpcSrv.Start()
	serveMetricsApi(nodeConfig.ListenAddr)
}

func serveMetricsApi(listenAddr string) {
	mux := http.NewServeMux()
	monitoring.RegisterMetrics(mux)
	exception.SafeGo("serveMetricsApi", func() {
		err := http.ListenAndServe(listenAddr, mux)
		if err != nil {
			logx.Error("NODE", fmt.Sprintf("Failed to expose metrics for monitoring: %v", err))
			os.Exit(1)
		}
	})
}
