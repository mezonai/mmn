// File: pkg/blockchain/blockchain.go
package blockchain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Block represents a single block in the blockchain.
type Block struct {
	Index            int            `json:"index"`
	Timestamp        int64          `json:"timestamp"`
	PrevHash         string         `json:"prev_hash"`
	Hash             string         `json:"hash"`
	Nonce            int            `json:"nonce"`
	RelationshipType string         `json:"relationship_type"`
	Receivers        []string       `json:"receivers"`
	TextData         string         `json:"text_data"`
	AudioData        string         `json:"audio_data"`
	VideoData        string         `json:"video_data"`
	Transactions     []*Transaction `json:"transactions"`
	SubBlocks        []*Block       `json:"sub_blocks"`
	Difficulty       int            `json:"difficulty"` // New field representing block difficulty.
	Category         string         `json:"category"`
}

// CalculateHash computes a SHA‑256 hash based on the block's data.
// The difficulty is now incorporated in the record to be hashed.
func CalculateHash(b *Block) string {
	record := fmt.Sprintf("%d%d%s%s%s%s%s%s%d%d",
		b.Index,
		b.Timestamp,
		b.PrevHash,
		b.RelationshipType,
		b.TextData,
		b.AudioData,
		b.VideoData,
		serializeReceivers(b.Receivers),
		b.Difficulty,
		b.Nonce,
		b.Category)
	h := sha256.Sum256([]byte(record))
	return hex.EncodeToString(h[:])
}

// serializeReceivers converts the slice of receivers into a string.
func serializeReceivers(receivers []string) string {
	return fmt.Sprintf("%v", receivers)
}

func MineBlock(b *Block, difficulty int) {
	target := strings.Repeat("0", difficulty)
	for {
		b.Hash = CalculateHash(b)
		if strings.HasPrefix(b.Hash, target) {
			break
		}
		b.Nonce++
	}
}

// CreateBlock constructs a new block given the necessary fields.
// It now sets a default difficulty (for example, 1). You could adjust this based on your PoW logic.
// Now it also takes a minerAddress and reward amount for the coinbase transaction.
func CreateBlock(index int, prevHash string, relationshipType string, receivers []string,
	text, audio, video string, txPool *TransactionPool, difficulty int, minerAddress string, reward float64) *Block {

	// Create a coinbase transaction for miner reward.
	coinbaseTx := NewTransaction("COINBASE", minerAddress, reward, 0)
	// Optionally, you could sign this transaction differently or leave it unsigned.
	// Prepend coinbase transaction to transaction pool.
	txPool.Transactions = append([]*Transaction{coinbaseTx}, txPool.Transactions...)

	block := &Block{
		Index:            index,
		Timestamp:        time.Now().Unix(),
		PrevHash:         prevHash,
		RelationshipType: relationshipType,
		Receivers:        receivers,
		TextData:         text,
		AudioData:        audio,
		VideoData:        video,
		Transactions:     txPool.Transactions,
		SubBlocks:        []*Block{},
		Difficulty:       difficulty,
		Nonce:            0,
		Category:         "main",
	}
	MineBlock(block, difficulty)
	return block
}

// Blockchain represents a chain of blocks.
type Blockchain struct {
	Blocks []*Block
}

// NewBlockchain creates and returns an empty blockchain.
func NewBlockchain() *Blockchain {
	return &Blockchain{
		Blocks: []*Block{},
	}
}

// AddBlock appends a new block to the blockchain.
func (bc *Blockchain) AddBlock(b *Block) {
	bc.Blocks = append(bc.Blocks, b)
	// Automatically prune the blockchain if it exceeds a certain size.
	const maxBlocks = 100 // for example
	if len(bc.Blocks) > maxBlocks {
		// Keep only the last 50 blocks.
		err := bc.PruneAndArchive(50, "archive")
		if err != nil {
			fmt.Println("Pruning error:", err)
		}
	}
}

// CumulativeDifficulty calculates the total difficulty of a chain.
func CumulativeDifficulty(chain []*Block) int {
	total := 0
	for _, b := range chain {
		total += b.Difficulty
	}
	return total
}

// IsValidChain verifies that the chain is valid.
func IsValidChain(chain []*Block) bool {
	if len(chain) == 0 {
		return false
	}

	// Validate the genesis block (assumed to have an empty PrevHash).
	if chain[0].PrevHash != "" || chain[0].Hash != CalculateHash(chain[0]) {
		return false
	}

	// Validate subsequent blocks.
	for i := 1; i < len(chain); i++ {
		current := chain[i]
		previous := chain[i-1]

		if current.PrevHash != previous.Hash {
			return false
		}
		if current.Hash != CalculateHash(current) {
			return false
		}
	}
	return true
}

// ReplaceChain replaces the current blockchain with newChain if newChain is valid
// and has a higher cumulative difficulty than the current chain.
func (bc *Blockchain) ReplaceChain(newChain []*Block) bool {
	if !IsValidChain(newChain) {
		return false
	}
	if CumulativeDifficulty(newChain) > CumulativeDifficulty(bc.Blocks) {
		bc.Blocks = newChain
		return true
	}
	return false
}

// UpdateBlockWithSubBlock simulates a change event on an existing block.
func (bc *Blockchain) UpdateBlockWithSubBlock(parentIndex int, newText, newAudio, newVideo, subBlockCategory string) {
	if parentIndex < 0 || parentIndex >= len(bc.Blocks) {
		fmt.Println("Invalid parent index")
		return
	}
	parentBlock := bc.Blocks[parentIndex]
	subBlock := &Block{
		Index:            parentBlock.Index,
		Timestamp:        time.Now().Unix(),
		PrevHash:         parentBlock.Hash,
		RelationshipType: parentBlock.RelationshipType,
		Receivers:        parentBlock.Receivers,
		TextData:         newText,
		AudioData:        newAudio,
		VideoData:        newVideo,
		Transactions:     []*Transaction{}, // Assuming no transactions for sub-block updates.
		SubBlocks:        []*Block{},
		Difficulty:       1, // Default difficulty; adjust if needed.
		Nonce:            0,
		Category:         subBlockCategory,
	}
	MineBlock(subBlock, subBlock.Difficulty)
	subBlock.Hash = CalculateHash(subBlock)
	parentBlock.SubBlocks = append(parentBlock.SubBlocks, subBlock)
}

// UpdateBlockWithSubBlockEx creates a sub-block with a specified category and appends it to the parent block.
func (bc *Blockchain) UpdateBlockWithSubBlockEx(parentIndex int, newText, newAudio, newVideo, subBlockCategory string) {
	if parentIndex < 0 || parentIndex >= len(bc.Blocks) {
		fmt.Println("Invalid parent index")
		return
	}
	parentBlock := bc.Blocks[parentIndex]
	subBlock := &Block{
		Index:            parentBlock.Index, // You can choose to assign a new index if preferred.
		Timestamp:        time.Now().Unix(),
		PrevHash:         parentBlock.Hash,
		RelationshipType: parentBlock.RelationshipType,
		Receivers:        parentBlock.Receivers,
		TextData:         newText,
		AudioData:        newAudio,
		VideoData:        newVideo,
		Transactions:     []*Transaction{}, // No transactions for sub-blocks by default.
		SubBlocks:        []*Block{},
		Difficulty:       1, // Default difficulty for sub-blocks.
		Nonce:            0,
		Category:         subBlockCategory, // e.g., "text", "metadata", "contract_state", "transaction_update"
	}
	// Mine the sub-block if you want to simulate PoW for sub-blocks.
	MineBlock(subBlock, subBlock.Difficulty)
	// Compute the sub-block's hash.
	subBlock.Hash = CalculateHash(subBlock)
	// Append the sub-block to the parent's SubBlocks slice.
	parentBlock.SubBlocks = append(parentBlock.SubBlocks, subBlock)
}

func GetBlockFromChain(bc *Blockchain, hash string) (*Block, error) {
	for _, b := range bc.Blocks {
		if b.Hash == hash {
			return b, nil
		}
	}
	return nil, fmt.Errorf("block not found")
}
