// File: integration_test.go
package blockchain_test

import (
	"testing"

	"cryptocypher/pkg/blockchain"
)

func TestChainReplacementWithHigherDifficulty(t *testing.T) {
	// Create the local blockchain.
	localChain := blockchain.NewBlockchain()
	txPool := &blockchain.TransactionPool{}
	difficulty := 3
	minerAddress := "Miner1"
	reward := 12.5

	// Create the genesis block for both chains.
	genesis := blockchain.CreateBlock(0, "", "one-to-one", []string{"ReceiverA"},
		"Text", "Audio", "Video", txPool, difficulty, minerAddress, reward)
	localChain.AddBlock(genesis)

	// Create an incoming blockchain that starts with the same genesis block.
	incomingChain := blockchain.NewBlockchain()
	incomingChain.AddBlock(genesis)

	// Clear the transaction pool (for simplicity).
	txPool.Clear()

	// Create a new block for the local chain.
	localBlock2 := blockchain.CreateBlock(1, genesis.Hash, "one-to-many", []string{"ReceiverA", "ReceiverB"},
		"Text", "Audio", "Video", txPool, difficulty, minerAddress, reward)
	localChain.AddBlock(localBlock2)

	// Create a new block for the incoming chain with a higher difficulty.
	incomingBlock2 := blockchain.CreateBlock(1, genesis.Hash, "one-to-many", []string{"ReceiverA", "ReceiverB"},
		"Text", "Audio", "Video", txPool, difficulty, minerAddress, reward)
	// Artificially increase difficulty to simulate more work.
	incomingBlock2.Difficulty = 5
	// Recalculate hash after modifying difficulty.
	incomingBlock2.Hash = blockchain.CalculateHash(incomingBlock2)
	incomingChain.AddBlock(incomingBlock2)

	// Now, localChain's cumulative difficulty is: 3 (genesis) + 3 (localBlock2) = 6.
	// IncomingChain's cumulative difficulty is: 3 (genesis) + 5 (incomingBlock2) = 8.
	// Therefore, localChain should be replaced by incomingChain.
	replaced := localChain.ReplaceChain(incomingChain.Blocks)
	if !replaced {
		t.Error("Expected chain replacement due to higher cumulative difficulty, but it did not occur.")
	}
}
