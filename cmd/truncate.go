package cmd

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/poh"
	"github.com/mezonai/mmn/types"

	"github.com/spf13/cobra"
)

const (
	database = "rocksdb"
)

type TruncateConfig struct {
	FromBlock       string
	TruncateDataDir string
}

var truncateConfig TruncateConfig

// transferCmd represents the transfer command
var truncateCmd = &cobra.Command{
	Use:   "truncate [flags]",
	Short: "Truncate blockchain",
	Long: `This command truncates the blockchain from the given block
Examples:
  # Truncate blockchain from block 100
  truncate -f 100 -d ./node-data/node1
`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := truncateBlockchain(); err != nil {
			logx.Error("TRUNCATE CLI", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(truncateCmd)
	truncateCmd.PersistentFlags().StringVarP(&truncateConfig.FromBlock, "from-block", "f", "0", "block to truncate from")
	truncateCmd.PersistentFlags().StringVarP(&truncateConfig.TruncateDataDir, "data-dir", "d", "./node-data/node1", "directory to truncate data")
}

func loadKeys() (pubKey string, privKey ed25519.PrivateKey, err error) {
	privKeyPath := filepath.Join(truncateConfig.TruncateDataDir, "privkey.txt")
	privKey, err = config.LoadEd25519PrivKey(privKeyPath)
	if err != nil {
		return "", nil, err
	}

	pubKey, err = config.LoadPubKeyFromPriv(privKeyPath)
	if err != nil {
		return "", nil, err
	}

	return pubKey, privKey, nil
}

func truncateBlockchain() error {
	// Connect rocksdb
	// Initialize db store inside directory
	storeDir := filepath.Join(truncateConfig.TruncateDataDir, "store")
	as, ts, tms, bs, err := initializeDBStore(storeDir, database, nil)
	if err != nil {
		logx.Error("TRUNCATE", "Failed to initialize blockstore:", err.Error())
		return err
	}
	logx.Info("TRUNCATE", "Blockstore initialized")

	defer bs.MustClose()
	defer ts.MustClose()
	defer tms.MustClose()
	defer as.MustClose()

	// load keys of current node
	pubKey, privKey, err := loadKeys()
	if err != nil {
		logx.Error("TRUNCATE", "Failed to load keys:", err.Error())
		return err
	}
	logx.Info("TRUNCATE", "Keys loaded")

	// Init config
	genesisPath := filepath.Join(truncateConfig.TruncateDataDir, "genesis.yml")
	genesisConfig, err := loadConfiguration(genesisPath)
	if err != nil {
		logx.Error("TRUNCATE", "Failed to load genesis configuration:", err.Error())
		return err
	}
	logx.Info("TRUNCATE", "Genesis configuration loaded")

	// 1. Get all blocks from given number to latest finalized block
	fromBlock, err := strconv.ParseUint(truncateConfig.FromBlock, 10, 64)
	if err != nil {
		logx.Error("TRUNCATE", "Failed to parse from block:", err.Error())
		return err
	}
	latestFinalizedSlot := bs.GetLatestFinalizedSlot()
	if fromBlock > latestFinalizedSlot {
		logx.Error("TRUNCATE", "From block is greater than latest finalized block")
		return fmt.Errorf("from block is greater than latest finalized block")
	}

	batch := make([]uint64, latestFinalizedSlot-fromBlock+1)
	for i := range batch {
		batch[i] = fromBlock + uint64(i)
	}
	blocks, err := bs.GetBatch(batch)
	if err != nil {
		logx.Error("TRUNCATE", "Failed to get blocks:", err.Error())
		return err
	}
	logx.Info("TRUNCATE", "Got", len(blocks), "blocks")

	// 2. Get all transactions and transaction metas from given block to latest finalized block
	txsHashes := make([]string, 0)
	allTxHashes := make(map[string]struct{})
	for _, block := range blocks {
		for _, entry := range block.Entries {
			for _, txHash := range entry.TxHashes {
				if _, exists := allTxHashes[txHash]; !exists {
					allTxHashes[txHash] = struct{}{}
					txsHashes = append(txsHashes, txHash)
				}
			}
		}
	}

	txs, err := ts.GetBatch(txsHashes)
	if err != nil {
		logx.Error("TRUNCATE", "Failed to get transactions:", err.Error())
		return err
	}
	txMetas, metaErr := tms.GetBatch(txsHashes)
	if metaErr != nil {
		logx.Error("TRUNCATE", "Failed to get transaction metas:", metaErr.Error())
		return err
	}
	logx.Info("TRUNCATE", "Got", len(txs), "transactions and", len(txMetas), "transaction metas")

	// 3. Remove blocks after fromBlock
	blocksToRemove := make([]uint64, 0)
	for i := fromBlock; i <= latestFinalizedSlot; i++ {
		blocksToRemove = append(blocksToRemove, i)
	}
	err = bs.RemoveBlocks(blocksToRemove)
	if err != nil {
		logx.Error("TRUNCATE", "Failed to remove blocks:", err.Error())
		return err
	}

	// 4. Build block at FromBlock with all transactions and transaction metas
	prevSlot := fromBlock - 1
	prevBlock := bs.Block(prevSlot)
	if prevBlock == nil {
		logx.Error("TRUNCATE", "Previous block is nil")
		return fmt.Errorf("previous block is nil")
	}
	prevHash := prevBlock.LastEntryHash()
	hashPerTick := genesisConfig.Poh.HashesPerTick
	tickPerSlot := genesisConfig.Poh.TicksPerSlot
	tickInterval := time.Duration(genesisConfig.Poh.TickIntervalMs) * time.Millisecond
	pohAutoHashInterval := tickInterval / 10

	pohEngine := poh.NewPoh(prevHash[:], &hashPerTick, pohAutoHashInterval)
	pohSchedule := config.ConvertLeaderSchedule(genesisConfig.LeaderSchedule)
	pohRecorder := poh.NewPohRecorder(pohEngine, tickPerSlot, pubKey, pohSchedule, prevSlot)

	// Simulate first entry contain all transactions
	_, err = pohRecorder.RecordTxs(txs)
	if err != nil {
		logx.Error("TRUNCATE", "Failed to record transactions:", err.Error())
		return err
	}
	for i := 1; i <= int(hashPerTick-2); i++ {
		pohEngine.HashOnce(pohEngine.Hash[:])
	}
	pohRecorder.Tick()

	// Simulate other entries contain only ticks
	for i := 1; i <= int(tickPerSlot-1); i++ {
		for i := 1; i <= int(hashPerTick-1); i++ {
			pohEngine.HashOnce(pohEngine.Hash[:])
		}
		pohRecorder.Tick()
	}

	entries := pohRecorder.DrainEntries()
	logx.Info("TRUNCATE", "Drained", len(entries), "entries")

	blk := block.AssembleBlock(
		fromBlock,
		prevHash,
		pubKey,
		entries,
	)
	blk.Sign(privKey)

	// Store block to database
	err = bs.ForceAddBlockPendingOnly(blk)
	if err != nil {
		logx.Error("TRUNCATE", "Failed to store block:", err.Error())
		return err
	}
	// Change status to finalize
	newBlock := bs.Block(fromBlock)
	err = bs.ForceMarkBlockAsFinalized(newBlock)
	if err != nil {
		logx.Error("TRUNCATE", "Failed to mark block as finalized:", err.Error())
		return err
	}

	// Point all transaction metas to the new block and save batch
	for _, txMeta := range txMetas {
		txMeta.Slot = fromBlock
		txMeta.BlockHash = hex.EncodeToString(blk.Hash[:])
	}
	txMetasList := make([]*types.TransactionMeta, 0, len(txMetas))
	for _, txMeta := range txMetas {
		txMetasList = append(txMetasList, txMeta)
	}
	err = tms.StoreBatch(txMetasList)
	if err != nil {
		logx.Error("TRUNCATE", "Failed to store transaction metas:", err.Error())
		return err
	}

	logx.Info("TRUNCATE", "Block stored successfully")
	return nil
}
