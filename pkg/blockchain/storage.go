// File: pkg/blockchain/storage.go
package blockchain

import (
	"encoding/json"
	"fmt"
	"log"

	bolt "go.etcd.io/bbolt"
)

const (
	dbName     = "blockchain.db"
	bucketName = "Blocks"
)

// DB is a wrapper around BoltDB for blockchain persistence.
type DB struct {
	*bolt.DB
}

// OpenDB opens or creates the BoltDB database.
func OpenDB() (*DB, error) {
	db, err := bolt.Open(dbName, 0600, nil)
	if err != nil {
		return nil, err
	}
	// Ensure the bucket exists.
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &DB{db}, nil
}

// SaveBlock saves a block into the database using its hash as the key.
func (db *DB) SaveBlock(b *Block) error {
	return db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketName))
		encoded, err := json.Marshal(b)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(b.Hash), encoded)
	})
}

// GetBlock retrieves a block from the database by its hash.
func (db *DB) GetBlock(hash string) (*Block, error) {
	var b Block
	err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketName))
		data := bucket.Get([]byte(hash))
		if data == nil {
			return fmt.Errorf("block not found")
		}
		return json.Unmarshal(data, &b)
	})
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// GetAllBlocks retrieves all blocks from the database.
func (db *DB) GetAllBlocks() ([]*Block, error) {
	var blocks []*Block
	err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketName))
		return bucket.ForEach(func(k, v []byte) error {
			var b Block
			if err := json.Unmarshal(v, &b); err != nil {
				return err
			}
			blocks = append(blocks, &b)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return blocks, nil
}

// LoadBlockchain loads the blockchain from the database and returns a Blockchain instance.
func (db *DB) LoadBlockchain() (*Blockchain, error) {
	blocks, err := db.GetAllBlocks()
	if err != nil {
		return nil, err
	}
	return &Blockchain{Blocks: blocks}, nil
}

// Close closes the database.
func (db *DB) Close() error {
	return db.DB.Close()
}

// TestStorage is a simple function to test the storage system.
func TestStorage() {
	db, err := OpenDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Assume we already have a block from CreateBlock.
	// Create a dummy transaction pool.
	txPool := &TransactionPool{}
	difficulty := 3

	// Define miner's address and reward for coinbase transaction.
	minerAddress := "Miner1"
	reward := 12.5

	// Create a block with PoW and coinbase transaction.
	block := CreateBlock(0, "", "one-to-one", []string{"ReceiverA"},
		"EncryptedText", "EncryptedAudio", "EncryptedVideo", txPool, difficulty, minerAddress, reward)

	if err := db.SaveBlock(block); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Block saved with hash:", block.Hash)

	retrieved, err := db.GetBlock(block.Hash)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Retrieved Block: %+v\n", retrieved)
}

// File: pkg/blockchain/storage.go (add this function)
func (db *DB) GetAllBlockHeaders() ([]LightBlockHeader, error) {
	var headers []LightBlockHeader
	err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketName))
		return bucket.ForEach(func(k, v []byte) error {
			var b Block
			if err := json.Unmarshal(v, &b); err != nil {
				return err
			}
			headers = append(headers, LightBlockHeader{
				Index:      b.Index,
				Timestamp:  b.Timestamp,
				PrevHash:   b.PrevHash,
				Hash:       b.Hash,
				Difficulty: b.Difficulty,
				Nonce:      b.Nonce,
			})
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return headers, nil
}
