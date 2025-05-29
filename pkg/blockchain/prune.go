// File: pkg/blockchain/prune.go
package blockchain

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"time"
)

// PruneAndArchive prunes the blockchain, keeping only the last retainCount blocks,
// and archives the older blocks to a file.
func (bc *Blockchain) PruneAndArchive(retainCount int, archiveFilename string) error {
	totalBlocks := len(bc.Blocks)
	if totalBlocks <= retainCount {
		// Nothing to prune.
		return nil
	}

	// Archive blocks older than the last retainCount blocks.
	archiveBlocks := bc.Blocks[:totalBlocks-retainCount]
	archiveData, err := json.MarshalIndent(archiveBlocks, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal archive blocks: %v", err)
	}

	// You might want to include a timestamp in the archive file name.
	archiveFile := fmt.Sprintf("%s_%d.json", archiveFilename, time.Now().Unix())
	err = ioutil.WriteFile(archiveFile, archiveData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write archive file: %v", err)
	}

	// Retain only the last retainCount blocks in memory.
	bc.Blocks = bc.Blocks[totalBlocks-retainCount:]
	fmt.Printf("Pruned blockchain: archived %d blocks to %s\n", totalBlocks-retainCount, archiveFile)
	return nil
}
