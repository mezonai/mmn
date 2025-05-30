// File: main.go
package main

import (
	"flag"
	"fmt"
	"net"
	"strings"
	"time"

	"mmm/pkg/api"
	"mmm/pkg/blockchain"
	"mmm/pkg/contract"
	"mmm/pkg/p2p"
)

func init() {
	// Register the static AdditionContract from the contract package.
	addition := contract.AdditionContract{}
	if err := contract.RegisterContract(addition); err != nil {
		fmt.Println("Error registering contract:", err)
	}
}
func getFreePort() (int, error) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.Port, nil
}

func main() {
	// Command-line flags for P2P configuration.
	defaultPort := 0
	port, err := getFreePort()
	if err != nil {
		fmt.Println("Failed to get a free port:", err)
		port = 8000 // fallback
	} else {
		defaultPort = port
	}
	listenAddr := flag.String("listenAddress", fmt.Sprintf("localhost:%d", defaultPort), "Address to listen on")
	rpcPort := flag.String("rpcPort", "8080", "Address to listen on") // should auto gen
	peerAddrs := flag.String("peerAddresses", "", "Comma-separated list of peer addresses")
	lightClient := flag.Bool("light", false, "Run in light client mode")
	flag.Parse()
	peers := []string{}
	if *peerAddrs != "" {
		peers = strings.Split(*peerAddrs, ",")
	}

	// Test Smart Contract Execution.
	result, err := contract.ExecuteContract("AdditionContract", "add", map[string]interface{}{"a": 10.0, "b": 15.5})
	if err != nil {
		fmt.Println("Contract execution error:", err)
	} else {
		fmt.Println("Contract execution result:", result)
	}

	// Initialize blockchain.
	var bc *blockchain.Blockchain
	if *lightClient {
		fmt.Println("Running in light client mode. Only block headers will be loaded.")
		bc = blockchain.NewBlockchain()
	} else {
		bc = blockchain.NewBlockchain()
	}

	// Create a transaction pool.
	txPool := &blockchain.TransactionPool{}

	// Create a ledger and initialize balances.
	ledger := blockchain.NewLedger()
	ledger["Alice"] = 100.0
	ledger["Bob"] = 50.0
	ledger["Charlie"] = 25.0

	// Add some transactions.
	tx1 := blockchain.NewTransaction("Alice", "Bob", 10.5, 1)
	tx2 := blockchain.NewTransaction("Bob", "Charlie", 5.25, 1)
	txPool.AddTransaction(tx1)
	txPool.AddTransaction(tx2)

	// Define relationship and receivers.
	relationshipType := "one-to-one"
	receivers := []string{"ReceiverA"}

	// Assume these are already encrypted payloads.
	textData := "EncryptedTextDataXYZ"
	audioData := "EncryptedAudioDataABC"
	videoData := "EncryptedVideoData123"

	// Set the difficulty for PoW.
	difficulty := 3
	// Miner address and reward.
	minerAddress := "Miner1"
	reward := 12.5

	// Create and add the genesis block with coinbase transaction.
	genesis := blockchain.CreateBlock(0, "", relationshipType, receivers, textData, audioData, videoData, txPool, difficulty, minerAddress, reward)
	bc.AddBlock(genesis)
	fmt.Println("Genesis Block Hash:", genesis.Hash)
	ledger.ProcessCoinbaseTransaction(minerAddress, reward)
	txPool.Clear()

	// Create a second block.
	tx3 := blockchain.NewTransaction("Charlie", "Alice", 3.75, 1)
	txPool.AddTransaction(tx3)
	relationshipType = "one-to-many"
	receivers = []string{"ReceiverA", "ReceiverB", "ReceiverC"}
	block2 := blockchain.CreateBlock(1, genesis.Hash, relationshipType, receivers, textData, audioData, videoData, txPool, difficulty, minerAddress, reward)
	bc.AddBlock(block2)
	fmt.Println("Block 2 Hash:", block2.Hash)
	ledger.ProcessCoinbaseTransaction(minerAddress, reward)
	txPool.Clear()

	// Add various sub-blocks to Block 2.
	bc.UpdateBlockWithSubBlockEx(1, "New Text Update", "", "", "text")
	bc.UpdateBlockWithSubBlockEx(1, "Metadata: Node updated", "", "", "metadata")
	bc.UpdateBlockWithSubBlockEx(1, "", "Contract state changed", "", "contract_state")
	bc.UpdateBlockWithSubBlockEx(1, "", "", "Transaction details updated", "transaction_update")
	fmt.Println("Block 2 now has", len(bc.Blocks[1].SubBlocks), "sub-block(s).")

	// Print blockchain summary.
	for _, blk := range bc.Blocks {
		fmt.Printf("Block %d (%s): Hash: %s, PrevHash: %s, RelType: %s, Receivers: %v\n",
			blk.Index, blk.Category, blk.Hash, blk.PrevHash, blk.RelationshipType, blk.Receivers)
		for i, sub := range blk.SubBlocks {
			fmt.Printf("  Sub-block %d (%s): Hash: %s, Timestamp: %d\n", i, sub.Category, sub.Hash, sub.Timestamp)
		}
	}

	// If running as a full node, start periodic pruning.
	if !*lightClient {
		go func() {
			for {
				time.Sleep(10 * time.Second)
				if len(bc.Blocks) > 100 {
					err := bc.PruneAndArchive(50, "archive")
					if err != nil {
						fmt.Println("Pruning error:", err)
					}
				}
			}
		}()
	}

	// Start auto-mining: periodically check the transaction pool and mine a new block if needed.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		for range ticker.C {
			if len(txPool.Transactions) > 0 {
				fmt.Println("Auto-mining triggered: pending transactions detected.")
				var prevHash string
				if len(bc.Blocks) > 0 {
					prevHash = bc.Blocks[len(bc.Blocks)-1].Hash
				}
				newBlock := blockchain.CreateBlock(len(bc.Blocks), prevHash, "one-to-many",
					[]string{"ReceiverA", "ReceiverB", "ReceiverC"}, textData, audioData, videoData,
					txPool, difficulty, minerAddress, reward)
				bc.AddBlock(newBlock)
				fmt.Println("Auto-mined Block Hash:", newBlock.Hash)
				ledger.ProcessCoinbaseTransaction(minerAddress, reward)
				txPool.Clear()
			}
		}
	}()

	// Hybrid Consensus: simulate block proposal and voting.
	hcm := blockchain.NewHybridConsensusManager()
	hcm.Stakeholders["Miner1"] = 50.0
	hcm.Stakeholders["Validator1"] = 30.0
	hcm.Stakeholders["Validator2"] = 20.0
	// Propose block2 as a candidate.
	hcm.ProposeBlock(block2)
	// Simulate validator votes.
	hcm.CastVote(0, "Validator1", true)
	hcm.CastVote(0, "Validator2", true)
	finalizedBlock := hcm.FinalizeBlock(100) // assuming total stake of 100 (50+30+20)
	if finalizedBlock != nil {
		fmt.Println("Finalized Block via Hybrid Consensus:", finalizedBlock.Hash)
	}

	// Dynamic Difficulty Adjustment: adjust difficulty periodically.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			newDifficulty := blockchain.AdjustDifficulty(bc.Blocks, 10*time.Second, 2)
			fmt.Println("Adjusted difficulty for next block:", newDifficulty)
		}
	}()

	// Sharding: initialize a beacon chain with 3 shards.
	beacon := blockchain.NewBeaconChain(3)
	// Process a sample transaction: assign tx1 to a shard.
	beacon.ProcessTransaction(tx1)

	// Start the P2P node.
	node := p2p.NewNode(*listenAddr, peers, bc)
	go node.Start()

	// Initialize the dynamic contract registry and start the API server.
	dynamicRegistry := contract.NewDynamicRegistry()
	apiServer := api.NewServer(bc, ledger, peers, dynamicRegistry)
	go apiServer.StartServer(*rpcPort)

	// Prevent main from exiting.
	select {}
}
