package config

// NodeConfig represents a node's configuration
type NodeConfig struct {
	PubKey             string   `yaml:"pubkey"`
	PrivKeyPath        string   `yaml:"privkey_path"`
	ListenAddr         string   `yaml:"listen_addr"`
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

type Faucet struct {
	Address string `yaml:"address"`
	Amount  uint64 `yaml:"amount"`
}

// GenesisConfig holds the configuration from genesis.yml
type GenesisConfig struct {
	LeaderSchedule []LeaderSchedule `yaml:"leader_schedule"`
	Faucet         Faucet           `yaml:"faucet"`
	Poh            PohConfig        `yaml:"poh"`
	Mempool        MempoolConfig    `yaml:"mempool"`
	Validator      ValidatorConfig  `yaml:"validator"`
}

// ConfigFile is the top-level structure for genesis.yml
type ConfigFile struct {
	Config GenesisConfig `yaml:"config"`
}
