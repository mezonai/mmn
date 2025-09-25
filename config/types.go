package config

import (
	"github.com/holiman/uint256"
)

// NodeConfig represents a node's configuration
type NodeConfig struct {
	PubKey             string   `yaml:"pubkey"`
	PrivKeyPath        string   `yaml:"privkey_path"`
	ListenAddr         string   `yaml:"listen_addr"`
	JSONRPCAddr        string   `yaml:"jsonrpc_addr"`
	Libp2pAddr         string   `yaml:"libp2p_addr"`
	GRPCAddr           string   `yaml:"grpc_addr"`
	BootStrapAddresses []string `yaml:"bootstrap_addresses"`
}

// LeaderSchedule represents a leader schedule entry
type LeaderSchedule struct {
	StartSlot int    `yaml:"start_slot"`
	EndSlot   int    `yaml:"end_slot"`
	Leader    string `yaml:"leader"`
}

type Alloc struct {
	Addresses []Address `yaml:"addresses"`
}

type Address struct {
	Address string       `yaml:"address"`
	Amount  *uint256.Int `yaml:"amount"`
}

type NativeCurrency struct {
	Decimals uint8 `yaml:"decimals"`
}

// GenesisConfig holds the configuration from genesis.yml
type GenesisConfig struct {
	LeaderSchedule []LeaderSchedule `yaml:"leader_schedule"`
	Alloc          Alloc            `yaml:"alloc"`
	Poh            PohConfig        `yaml:"poh"`
	Mempool        MempoolConfig    `yaml:"mempool"`
	Validator      ValidatorConfig  `yaml:"validator"`
	NativeCurrency NativeCurrency   `yaml:"native_currency"`
}

// ConfigFile is the top-level structure for genesis.yml
type ConfigFile struct {
	Config GenesisConfig `yaml:"config"`
}
