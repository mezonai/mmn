package config

import (
	"crypto/ed25519"
	"encoding/hex"
	"io/ioutil"
	"log"
	"mmn/poh"
	"os"

	"gopkg.in/ini.v1"
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

type PohConfig struct {
	HashesPerTick  uint64 `ini:"hashes_per_tick"`
	TicksPerSlot   uint64 `ini:"ticks_per_slot"`
	TickIntervalMs int    `ini:"tick_interval_ms"`
}

type MempoolConfig struct {
	MaxTxs int `ini:"max_txs"`
}

type ValidatorConfig struct {
	BatchSize                 int `ini:"batch_size"`
	LeaderTimeout             int `ini:"leader_timeout"`
	LeaderTimeoutLoopInterval int `ini:"leader_timeout_loop_interval"`
}

// LoadPohConfig reads PoH config from an .ini file
func LoadPohConfig(path string) (*PohConfig, error) {
	cfg, err := ini.Load(path)
	if err != nil {
		return nil, err
	}
	pohSection := cfg.Section("poh")
	pohCfg := &PohConfig{}
	err = pohSection.MapTo(pohCfg)
	if err != nil {
		return nil, err
	}
	return pohCfg, nil
}

func LoadMempoolConfig(path string) (*MempoolConfig, error) {
	cfg, err := ini.Load(path)
	if err != nil {
		return nil, err
	}
	mempoolSection := cfg.Section("mempool")
	mempoolCfg := &MempoolConfig{}
	err = mempoolSection.MapTo(mempoolCfg)
	if err != nil {
		return nil, err
	}
	return mempoolCfg, nil
}

func LoadValidatorConfig(path string) (*ValidatorConfig, error) {
	cfg, err := ini.Load(path)
	if err != nil {
		return nil, err
	}
	validatorSection := cfg.Section("validator")
	validatorCfg := &ValidatorConfig{}
	err = validatorSection.MapTo(validatorCfg)
	if err != nil {
		return nil, err
	}
	return validatorCfg, nil
}
