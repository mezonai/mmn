package config

import (
	"crypto/ed25519"
	"encoding/hex"
	"io/ioutil"
	"log"
	"mmn/poh"
	"os"

	"gopkg.in/yaml.v3"
)

// NodeConfig represents a node's configuration
type NodeConfig struct {
	PubKey      string `yaml:"pubkey"`
	PrivKeyPath string `yaml:"privkey_path"`
	ListenAddr  string `yaml:"listen_addr"`
	GRPCAddr    string `yaml:"grpc_addr"`
}

// LeaderSchedule represents a leader schedule entry
type LeaderSchedule struct {
	StartSlot int    `yaml:"start_slot"`
	EndSlot   int    `yaml:"end_slot"`
	Leader    string `yaml:"leader"`
}

// GenesisConfig holds the configuration from genesis.yml
type GenesisConfig struct {
	SelfNode       NodeConfig       `yaml:"self_node"`
	PeerNodes      []NodeConfig     `yaml:"peer_nodes"`
	LeaderSchedule []LeaderSchedule `yaml:"leader_schedule"`
}

// ConfigFile is the top-level structure for genesis.yml
type ConfigFile struct {
	Config GenesisConfig `yaml:"config"`
}

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
	log.Printf("[config] Successfully loaded config: SelfNode=%+v, PeerNodes=%d, LeaderSchedule=%d entries", cfgFile.Config.SelfNode, len(cfgFile.Config.PeerNodes), len(cfgFile.Config.LeaderSchedule))
	return &cfgFile.Config, nil
}

// LoadEd25519PrivKey loads an Ed25519 private key from a file (expects hex encoding)
func LoadEd25519PrivKey(path string) (ed25519.PrivateKey, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := hex.DecodeString(string(data))
	if err != nil {
		return nil, err
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, err
	}
	return ed25519.PrivateKey(key), nil
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
