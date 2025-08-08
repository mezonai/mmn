package main

import (
	"log"
	"time"

	"mmn/api"
	"mmn/blockstore"
	"mmn/config"
	"mmn/consensus"
	"mmn/ledger"
	"mmn/logx"
	"mmn/mempool"
	"mmn/network"
	"mmn/poh"
	"mmn/validator"

	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()

	// load node config
	cfg, self, seed, peers, leaderSchedule, privKey := config.NewConfig("node3")

	// --- Blockstore ---
	blockDir := "./blockstore/blocks"
	bs, err := blockstore.NewBlockStore(blockDir, seed)

	if err != nil {
		logx.Error("BLOCK STORE", "Failed to init blockstore: ", err)
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

	// network libp2p
	libp2pNetwork, err := network.NewNetWork(
		self.PubKey,
		privKey,
		self.Libp2pAddr,
		"/ip4/10.10.30.50/tcp/56363/p2p/12D3KooWDqiNAU1nKWm67TEH6852QKBxkwQ6sikUvWV6f2camNxr",
		bs,
	)

	if err != nil {
		logx.Error("Failed to create libp2p network: %v", err)
	}

	mp := mempool.NewMempool(1000, libp2pNetwork)

	leaderBatchLoopInterval := tickInterval / 2
	roleMonitorLoopInterval := tickInterval
	batchSize := 100
	leaderTimeout := 50 * time.Millisecond
	leaderTimeoutLoopInterval := 5 * time.Millisecond

	val := validator.NewValidator(
		self.PubKey, privKey, recorder, pohService, pohSchedule, mp, ticksPerSlot,
		leaderBatchLoopInterval, roleMonitorLoopInterval, leaderTimeout, leaderTimeoutLoopInterval, batchSize, libp2pNetwork, bs, ld, collector,
	)

	libp2pNetwork.SetupCallbacks(ld, privKey, self, bs, collector, mp)

	// --- Start validator ---
	val.Run()

	// --- API (for tx submission) ---
	apiSrv := api.NewAPIServer(mp, ld, self.ListenAddr)
	apiSrv.Start()

	// --- Block forever ---
	select {}
}
