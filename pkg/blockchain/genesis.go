// File: pkg/blockchain/genesis.go
package blockchain

import (
	"fmt"
	"time"

	"github.com/mezonai/mmn/config"
)

// CreateGenesisBlock creates the genesis block with faucet initialization
func CreateGenesisBlock(cfg *config.GenesisConfig) *Block {
	// Create genesis transaction pool with faucet transaction
	txPool := &TransactionPool{}

	// Create faucet genesis transaction using transaction.Transaction
	faucetTx := &Transaction{
		Sender:    "GENESIS", // Special sender for genesis
		Recipient: cfg.Faucet.Address,
		Amount:    float64(cfg.Faucet.Amount),
		Timestamp: time.Now().Unix(),
		Nonce:     0,
		Signature: "GENESIS_SIGNATURE", // Special signature for genesis
	}

	txPool.AddTransaction(faucetTx)

	// Create genesis block
	genesisBlock := &Block{
		Index:            0,
		Timestamp:        time.Now().Unix(),
		PrevHash:         "", // Genesis has no previous hash
		RelationshipType: "genesis",
		Receivers:        []string{cfg.Faucet.Address},
		TextData:         "Genesis Block - Faucet Initialization",
		AudioData:        "",
		VideoData:        "",
		Transactions:     txPool.Transactions,
		SubBlocks:        []*Block{},
		Difficulty:       1, // Genesis block has minimal difficulty
		Nonce:            0,
		Category:         "genesis",
	}

	// Calculate genesis block hash
	genesisBlock.Hash = CalculateHash(genesisBlock)

	return genesisBlock
}

// NewBlockchainWithGenesis creates a new blockchain with genesis block
func NewBlockchainWithGenesis(cfg *config.GenesisConfig) *Blockchain {
	genesisBlock := CreateGenesisBlock(cfg)

	blockchain := &Blockchain{
		Blocks: []*Block{genesisBlock},
	}

	return blockchain
}

// ProcessGenesisTransactions processes all transactions in the genesis block
// and updates the ledger accordingly
func ProcessGenesisTransactions(genesisBlock *Block, ledger LedgerInterface) error {
	for _, tx := range genesisBlock.Transactions {
		if tx.Sender == "GENESIS" {
			// Genesis transactions create accounts with initial balances
			err := ledger.CreateAccountFromGenesis(tx.Recipient, uint64(tx.Amount))
			if err != nil {
				return fmt.Errorf("failed to process genesis transaction: %w", err)
			}
		}
	}
	return nil
}

// LedgerInterface defines the interface for ledger operations
type LedgerInterface interface {
	CreateAccountFromGenesis(address string, balance uint64) error
	AccountExists(address string) bool
}

// IsGenesisBlock checks if a block is the genesis block
func IsGenesisBlock(block *Block) bool {
	return block.Index == 0 && block.PrevHash == "" && block.Category == "genesis"
}

// ValidateGenesisBlock validates the genesis block structure
func ValidateGenesisBlock(block *Block, cfg *config.GenesisConfig) error {
	if !IsGenesisBlock(block) {
		return fmt.Errorf("not a genesis block")
	}

	if len(block.Transactions) == 0 {
		return fmt.Errorf("genesis block must contain at least one transaction")
	}

	// Validate faucet transaction exists
	faucetTxFound := false
	for _, tx := range block.Transactions {
		if tx.Sender == "GENESIS" && tx.Recipient == cfg.Faucet.Address {
			if uint64(tx.Amount) != cfg.Faucet.Amount {
				return fmt.Errorf("genesis faucet amount mismatch: expected %d, got %f", cfg.Faucet.Amount, tx.Amount)
			}
			faucetTxFound = true
			break
		}
	}

	if !faucetTxFound {
		return fmt.Errorf("genesis block missing faucet transaction")
	}

	return nil
}
