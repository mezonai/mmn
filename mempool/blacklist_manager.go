package mempool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mezonai/mmn/logx"
)

type BlacklistEntry struct {
	Address string `json:"address"`
	Reason  string `json:"reason"`
}

type BlacklistData struct {
	Wallets []BlacklistEntry `json:"wallets"`
}

type BlacklistManager struct {
	mu       sync.RWMutex
	filePath string
}

func NewBlacklistManager(dataDir string) *BlacklistManager {
	blacklistPath := filepath.Join(dataDir, "blacklist.json")
	return &BlacklistManager{
		filePath: blacklistPath,
	}
}

func (bm *BlacklistManager) SaveBlacklistToFile(blacklist map[string]string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	wallets := make([]BlacklistEntry, 0, len(blacklist))
	for address, reason := range blacklist {
		wallets = append(wallets, BlacklistEntry{
			Address: address,
			Reason:  reason,
		})
	}

	data := BlacklistData{
		Wallets: wallets,
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(bm.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create blacklist directory: %w", err)
	}

	tempPath := bm.filePath + ".tmp"
	file, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temporary blacklist file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to encode blacklist data: %w", err)
	}

	file.Close()

	if err := os.Rename(tempPath, bm.filePath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to rename temporary blacklist file: %w", err)
	}

	logx.Info("BLACKLIST", fmt.Sprintf("Successfully saved %d blacklist wallets to %s", len(wallets), bm.filePath))
	return nil
}

func (bm *BlacklistManager) LoadBlacklistFromFile() (map[string]string, error) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	if _, err := os.Stat(bm.filePath); os.IsNotExist(err) {
		logx.Info("BLACKLIST", "Blacklist file does not exist, starting with empty blacklist")
		return make(map[string]string), nil
	}

	file, err := os.Open(bm.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open blacklist file: %w", err)
	}
	defer file.Close()

	var data BlacklistData
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode blacklist data: %w", err)
	}

	blacklist := make(map[string]string, len(data.Wallets))
	for _, entry := range data.Wallets {
		blacklist[entry.Address] = entry.Reason
	}

	logx.Info("BLACKLIST", fmt.Sprintf("Successfully loaded %d blacklist entries from %s", len(blacklist), bm.filePath))
	return blacklist, nil
}
