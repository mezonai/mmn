package cmd

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mezonai/mmn/monitoring"
	"github.com/mezonai/mmn/types"
	"github.com/mezonai/mmn/utils"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/poh"
	"github.com/spf13/cobra"
)

const ZeroAddress = "0000000000000000000000000000000000000000000000000000000000000000"

var (
	// Init command specific variables
	initGenesisPath string
	initDataDir     string
	initDatabase    string
	initPrivKeyPath string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize node by generating a private key and genesis block",
	Long: `Initialize a new blockchain node by:
- Generating a new Ed25519 private key (or using provided one)
- Creating genesis block from configuration file
- Setting up data directory structure
- Initializing blockstore with chosen database backend`,
	Run: func(cmd *cobra.Command, args []string) {
		initializeNode()
	},
}

func init() {
	// Add init command to root
	rootCmd.AddCommand(initCmd)

	// Init command flags
	initCmd.Flags().StringVar(&initGenesisPath, "genesis", "config/genesis.yml", "Path to genesis configuration file")
	initCmd.Flags().StringVar(&initDataDir, "data-dir", ".", "Directory to save node data")
	initCmd.Flags().StringVar(&initDatabase, "database", "leveldb", "Database backend (leveldb or rocksdb)")
	initCmd.Flags().StringVar(&initPrivKeyPath, "privkey-path", "", "Path to existing private key file (optional)")
}

// initializeNode generates a new Ed25519 seed, creates genesis block, and initializes node data.
// This method is idempotent and safe to run multiple time
func initializeNode() {
	initializeFileLogger()
	monitoring.InitMetrics()

	// Ensure data directory exists
	if err := os.MkdirAll(initDataDir, 0o755); err != nil {
		logx.Error("INIT", "Failed to create data directory:", err.Error())
		return
	}

	// Create full paths for key files
	privKeyFile := filepath.Join(initDataDir, "privkey.txt")
	pubKeyFile := filepath.Join(initDataDir, "pubkey.txt")

	var pubKeyHex string
	var err error

	// Check if privkey-path is provided
	if initPrivKeyPath != "" {
		// Use provided private key file
		logx.Info("INIT", "Using provided private key from:", initPrivKeyPath)

		// Load public key from provided private key
		pubKeyHex, err = config.LoadPubKeyFromPriv(initPrivKeyPath)
		if err != nil {
			logx.Error("INIT", "Failed to load public key from provided private key:", err.Error())
			return
		}

		// Copy the private key to data directory
		var privKeyData []byte
		privKeyData, err = os.ReadFile(initPrivKeyPath)
		if err != nil {
			logx.Error("INIT", "Failed to read provided private key file:", err.Error())
			return
		}

		err = os.WriteFile(privKeyFile, privKeyData, 0o600)
		if err != nil {
			logx.Error("INIT", "Failed to copy private key to data directory:", err.Error())
			return
		}

		// Save public key
		err = os.WriteFile(pubKeyFile, []byte(pubKeyHex), 0o644)
		if err != nil {
			logx.Error("INIT", "Failed to write public key to file:", err.Error())
			return
		}

		logx.Info("INIT", "Private key copied to:", privKeyFile)
		logx.Info("INIT", "Public key saved to:", pubKeyFile)
	} else {
		// Check if both private and public key files already exist
		privKeyExists := false
		pubKeyExists := false
		if _, err = os.Stat(privKeyFile); err == nil {
			privKeyExists = true
		}
		if _, err = os.Stat(pubKeyFile); err == nil {
			pubKeyExists = true
		}

		if privKeyExists && pubKeyExists {
			logx.Info("INIT", "Private and public key files already exist, skipping key generation")
			// Load existing public key
			_, err = config.LoadPubKeyFromPriv(privKeyFile)
			if err != nil {
				logx.Error("INIT", "Failed to load existing public key:", err.Error())
				return
			}
		} else {
			// Generate new keys
			logx.Info("INIT", "Generating new Ed25519 key pair")

			// Generate 32-byte Ed25519 seed
			seed := make([]byte, ed25519.SeedSize)
			_, err = rand.Read(seed)
			if err != nil {
				logx.Error("INIT", "Failed to generate Ed25519 seed:", err.Error())
				return
			}

			// Generate key pair from seed
			privKey := ed25519.NewKeyFromSeed(seed)
			pubKey := privKey.Public().(ed25519.PublicKey)

			// Convert seed to hex string for private key file
			seedHex := hex.EncodeToString(seed)

			// Save private key
			err = os.WriteFile(privKeyFile, []byte(seedHex), 0o600)
			if err != nil {
				logx.Error("INIT", "Failed to write private key to file:", err.Error())
				return
			}

			// Save public key
			pubKeyHex = hex.EncodeToString(pubKey)
			err = os.WriteFile(pubKeyFile, []byte(pubKeyHex), 0o644)
			if err != nil {
				logx.Error("INIT", "Failed to write public key to file:", err.Error())
				return
			}

			logx.Info("INIT", "Successfully generated Ed25519 key pair")
			logx.Info("INIT", "Private key saved to:", privKeyFile)
			logx.Info("INIT", "Public key saved to:", pubKeyFile)
		}
	}

	// Load genesis configuration
	cfg, err := loadConfiguration(initGenesisPath)
	if err != nil {
		logx.Error("INIT", "Failed to load genesis configuration:", err.Error())
		return
	}

	// Copy genesis.yml to data directory
	genesisDestPath := filepath.Join(initDataDir, "genesis.yml")
	genesisData, err := os.ReadFile(initGenesisPath)
	if err != nil {
		logx.Error("INIT", "Failed to read genesis configuration file:", err.Error())
		return
	}

	err = os.WriteFile(genesisDestPath, genesisData, 0o644)
	if err != nil {
		logx.Error("INIT", "Failed to copy genesis configuration to data directory:", err.Error())
		return
	}

	logx.Info("INIT", "Genesis configuration copied to:", genesisDestPath)

	// Initialize db store inside directory
	dbStoreDir := filepath.Join(initDataDir, "store")
	as, ts, tms, bs, err := initializeDBStore(dbStoreDir, initDatabase, nil)
	if err != nil {
		logx.Error("INIT", "Failed to initialize db store:", err.Error())
		return
	}
	defer bs.MustClose()
	defer ts.MustClose()
	defer tms.MustClose()
	defer as.MustClose()

	// Initialize ledger
	ld := ledger.NewLedger(bs, ts, tms, as, nil, nil)

	// Check if genesis block already exists then skip creation
	if bs.HasCompleteBlock(0) {
		logx.Info("INIT", "Genesis block already exists, skipping creation")
		return
	}

	// Create genesis block using AssembleBlock
	genesisBlock, err := initializeBlockchainWithGenesis(cfg, ld)
	if err != nil {
		logx.Error("INIT", "Failed to initialize blockchain with genesis:", err.Error())
		return
	}

	// Persist genesis block to database
	if genesisBlock != nil {
		err = bs.AddBlockPending(genesisBlock)
		if err != nil {
			logx.Error("INIT", "Failed to persist genesis block to database:", err.Error())
			return
		}

		// Finalizegenesis block
		txMeta := map[string]*types.TransactionMeta{}
		addrAccount := map[string]*types.Account{}
		latestVersionContentHashMap := map[string]string{}
		err = bs.FinalizeBlock(utils.BroadcastedBlockToBlock(genesisBlock), txMeta, addrAccount, latestVersionContentHashMap)
		if err != nil {
			logx.Error("INIT", "Failed to finalize genesis block :", err.Error())
			return
		}

		logx.Info("INIT", "Genesis block created with hash:", fmt.Sprintf("%x", genesisBlock.Hash))
		logx.Info("INIT", "Genesis block contains", len(genesisBlock.Entries), "PoH entries")
		logx.Info("INIT", "Genesis block slot:", genesisBlock.Slot)
		logx.Info("INIT", "Genesis block persisted to database and marked as finalized")
	} else {
		logx.Info("INIT", "Genesis block already exists, skipping creation")
	}

	logx.Info("INIT", "Node initialization completed successfully")
	logx.Info("INIT", "Private key saved to:", privKeyFile)
	logx.Info("INIT", "Data directory:", initDataDir)
	logx.Info("INIT", "Database backend:", initDatabase)
	logx.Info("INIT", "Genesis configuration loaded from:", initGenesisPath)
}

// initializeBlockchainWithGenesis initializes blockchain with genesis block using AssembleBlock
func initializeBlockchainWithGenesis(cfg *config.GenesisConfig, ld *ledger.Ledger) (*block.BroadcastedBlock, error) {
	// Create genesis PoH entry with alloc transaction
	// For genesis, we create a simple entry with the alloc initialization
	var genesisHash [32]byte                         // Zero hash for genesis
	genesisEntry := poh.NewTickEntry(1, genesisHash) // Simple tick entry for genesis

	// Use AssembleBlock to create the genesis block
	genesisBlock := block.AssembleBlock(
		0,                         // slot 0 for genesis
		genesisHash,               // previous hash is zero for genesis
		ZeroAddress,               // use alloc address as leader for genesis
		[]poh.Entry{genesisEntry}, // genesis entry
	)

	// Process genesis alloc account creation
	err := ld.CreateAccountsFromGenesis(cfg.Alloc.Addresses)
	if err != nil {
		return nil, fmt.Errorf("failed to create genesis alloc account: %w", err)
	}

	logx.Info("GENESIS", fmt.Sprintf("Successfully initialized genesis block using AssembleBlock with alloc account %s", cfg.Alloc.Addresses))
	logx.Info("GENESIS", "Ledger snapshot saved to ledger/snapshot.gob")
	logx.Info("GENESIS", fmt.Sprintf("Genesis block hash: %x", genesisBlock.Hash))

	return genesisBlock, nil
}

func initializeFileLogger() {
	// Load config log file name
	logFile := "./logs/mmn.log"
	if logFileConfig := os.Getenv("LOGFILE"); logFileConfig != "" {
		logFile = "./logs/" + logFileConfig
	}

	// Load config log file size
	maxSizeConfig := os.Getenv("LOGFILE_MAX_SIZE_MB")
	if maxSizeConfig == "" {
		panic("LOGFILE_MAX_SIZE_MB env variable not set")
	}
	maxSizeMB, err := strconv.Atoi(maxSizeConfig)
	if err != nil {
		panic("Invalid value for LOGFILE_MAX_SIZE_MB" + err.Error())
	}

	// Load config log file
	maxAgeConfig := os.Getenv("LOGFILE_MAX_AGE_DAYS")
	if maxAgeConfig == "" {
		panic("LOGFILE_MAX_AGE_DAYS env variable not set")
	}
	maxAgeDays, err := strconv.Atoi(maxAgeConfig)
	if err != nil {
		panic("Invalid value for LOGFILE_MAX_AGE_DAYS" + err.Error())
	}

	lumberjackLogger := &lumberjack.Logger{
		Filename: logFile,
		MaxSize:  maxSizeMB,
		MaxAge:   maxAgeDays,
	}

	logx.InitWithOutput(lumberjackLogger)
}
