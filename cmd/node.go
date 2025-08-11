package cmd

import (
	"crypto/ed25519"
	"encoding/hex"
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

	"github.com/spf13/cobra"
)

var nodeName string

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the blockchain node",
	Run: func(cmd *cobra.Command, args []string) {
		runNode(nodeName)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringVarP(&nodeName, "node", "n", "node1", "The node to run")
}

func runNode(currentNode string) {
	// --- Load config from genesis.yml ---
	cfg, err := config.LoadGenesisConfig(fmt.Sprintf("config/genesis.%s.yml", currentNode))
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

	// --- Load private key ---
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

	pohEngine := poh.NewPoh(seed, &hashesPerTick, pohAutoHashInterval)
	pohEngine.Run()

	pohSchedule := config.ConvertLeaderSchedule(leaderSchedule)
	recorder := poh.NewPohRecorder(pohEngine, ticksPerSlot, self.PubKey, pohSchedule)

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

	// --- Mempool ---
	mp := mempool.NewMempool(1000, netClient)

	// --- Validator ---
	leaderBatchLoopInterval := tickInterval / 2
	roleMonitorLoopInterval := tickInterval
	batchSize := 100
	leaderTimeout := 50 * time.Millisecond
	leaderTimeoutLoopInterval := 5 * time.Millisecond

	val := validator.NewValidator(
		self.PubKey, privKey, recorder, pohService, pohSchedule, mp, ticksPerSlot,
		leaderBatchLoopInterval, roleMonitorLoopInterval, leaderTimeout, leaderTimeoutLoopInterval, batchSize, netClient, bs, ld, collector,
	)
	val.Run()

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
	_ = grpcSrv // keep server running

	// --- API ---
	apiSrv := api.NewAPIServer(mp, ld, self.ListenAddr)
	apiSrv.Start()

	select {}
}
