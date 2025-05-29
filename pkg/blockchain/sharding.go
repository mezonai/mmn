// File: pkg/blockchain/sharding.go
package blockchain

import (
	"crypto/sha256"
	"fmt"
)

// Shard represents a partition of the blockchain.
type Shard struct {
	ID         int
	Blockchain *Blockchain
}

// BeaconChain coordinates multiple shards.
type BeaconChain struct {
	Shards []*Shard
}

// NewBeaconChain initializes a beacon chain with the specified number of shards.
func NewBeaconChain(numShards int) *BeaconChain {
	shards := make([]*Shard, numShards)
	for i := 0; i < numShards; i++ {
		shards[i] = &Shard{
			ID:         i,
			Blockchain: NewBlockchain(),
		}
	}
	return &BeaconChain{
		Shards: shards,
	}
}

// AssignShard assigns a transaction to a shard based on the sender's address.
func (bc *BeaconChain) AssignShard(tx *Transaction) int {
	hash := sha256.Sum256([]byte(tx.Sender))
	shardID := int(hash[0]) % len(bc.Shards)
	return shardID
}

// ProcessTransaction assigns and processes a transaction in the appropriate shard.
func (bc *BeaconChain) ProcessTransaction(tx *Transaction) {
	shardID := bc.AssignShard(tx)
	fmt.Printf("Assigning transaction from %s to shard %d\n", tx.Sender, shardID)
	// Here, you'd add the transaction to the shard's transaction pool or process it.
	// For demonstration, we just print a message.
}
