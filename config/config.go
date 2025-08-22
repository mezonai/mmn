package config

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/mezonai/mmn/pkg/common"
	"github.com/mezonai/mmn/poh"

	"gopkg.in/yaml.v3"
)

// LoadGenesisConfig reads and parses the genesis.yml file
func LoadGenesisConfig(path string) (*GenesisConfig, error) {
	log.Printf("[config] LoadGenesisConfig called with path: %s", path)
	file, err := os.Open(path)
	if err != nil {
		log.Printf("[config] Failed to open file: %v", err)
		return nil, err
	}
	log.Printf("[config] Opened file: %s", path)
	defer file.Close()

	var cfgFile ConfigFile
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&cfgFile); err != nil {
		log.Printf("[config] Failed to decode YAML: %v", err)
		return nil, err
	}
	log.Printf("[config] Successfully loaded config: LeaderSchedule=%d entries, Faucet=%+v", len(cfgFile.Config.LeaderSchedule), cfgFile.Config.Alloc)
	return &cfgFile.Config, nil
}

// LoadEd25519PrivKey loads an Ed25519 private key from a file (accepts base58 or hex)
func LoadEd25519PrivKey(path string) (ed25519.PrivateKey, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	keyHex := strings.TrimSpace(string(data))
	privBytes, err := common.DecodeBase58ToBytes(keyHex)
	if err != nil || len(privBytes) == 0 {
		// Fallback to hex
		hb, herr := hex.DecodeString(keyHex)
		if herr != nil {
			return nil, fmt.Errorf("failed to decode private key as base58 or hex: %v | %v", err, herr)
		}
		privBytes = hb
	}

	var privKey ed25519.PrivateKey
	if len(privBytes) == ed25519.SeedSize {
		// 32-byte seed, generate full private key
		privKey = ed25519.NewKeyFromSeed(privBytes)
	} else if len(privBytes) == ed25519.PrivateKeySize {
		// 64-byte full private key
		privKey = ed25519.PrivateKey(privBytes)
	} else {
		return nil, fmt.Errorf("invalid ed25519 private key length: %d, expected %d (seed) or %d (full key)", len(privBytes), ed25519.SeedSize, ed25519.PrivateKeySize)
	}

	return privKey, nil
}

// ConvertLeaderSchedule converts []config.LeaderSchedule to *poh.LeaderSchedule
func ConvertLeaderSchedule(entries []LeaderSchedule) *poh.LeaderSchedule {
	pohEntries := make([]poh.LeaderScheduleEntry, len(entries))
	for i, e := range entries {
		pohEntries[i] = poh.LeaderScheduleEntry{
			StartSlot: uint64(e.StartSlot),
			EndSlot:   uint64(e.EndSlot),
			Leader:    e.Leader,
		}
	}
	ls, err := poh.NewLeaderSchedule(pohEntries)
	if err != nil {
		log.Fatalf("Invalid leader schedule: %v", err)
	}
	return ls
}

type PohConfig struct {
	HashesPerTick  uint64 `yaml:"hashes_per_tick"`
	TicksPerSlot   uint64 `yaml:"ticks_per_slot"`
	TickIntervalMs int    `yaml:"tick_interval_ms"`
}

type MempoolConfig struct {
	MaxTxs int `yaml:"max_txs"`
}

type ValidatorConfig struct {
	BatchSize                 int `yaml:"batch_size"`
	LeaderTimeout             int `yaml:"leader_timeout"`
	LeaderTimeoutLoopInterval int `yaml:"leader_timeout_loop_interval"`
}

// LoadPohConfig reads PoH config from genesis.yml file
func LoadPohConfig(path string) (*PohConfig, error) {
	genesisCfg, err := LoadGenesisConfig(path)
	if err != nil {
		return nil, err
	}
	return &genesisCfg.Poh, nil
}

func LoadMempoolConfig(path string) (*MempoolConfig, error) {
	genesisCfg, err := LoadGenesisConfig(path)
	if err != nil {
		return nil, err
	}
	return &genesisCfg.Mempool, nil
}

func LoadValidatorConfig(path string) (*ValidatorConfig, error) {
	genesisCfg, err := LoadGenesisConfig(path)
	if err != nil {
		return nil, err
	}
	return &genesisCfg.Validator, nil
}

func LoadPubKeyFromPriv(privKeyPath string) (string, error) {
	data, err := os.ReadFile(privKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to read private key file: %w", err)
	}

	keyHex := strings.TrimSpace(string(data))

	privBytes, err := common.DecodeBase58ToBytes(keyHex)
	if err != nil || len(privBytes) == 0 {
		// Fallback to hex
		hb, herr := hex.DecodeString(keyHex)
		if herr != nil {
			return "", fmt.Errorf("failed to decode private key (base58 and hex): %v | %v", err, herr)
		}
		privBytes = hb
	}

	var privKey ed25519.PrivateKey
	if len(privBytes) == ed25519.SeedSize {
		privKey = ed25519.NewKeyFromSeed(privBytes)
	} else if len(privBytes) == ed25519.PrivateKeySize {
		privKey = ed25519.PrivateKey(privBytes)
	} else {
		return "", fmt.Errorf("invalid ed25519 private key length: %d", len(privBytes))
	}

	pubKey := privKey.Public().(ed25519.PublicKey)

	return common.EncodeBytesToBase58(pubKey), nil
}
