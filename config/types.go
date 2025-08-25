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

type Alloc struct {
	Addresses []Address `yaml:"addresses"`
}

type Address struct {
	Address string `yaml:"address"`
	Amount  uint64 `yaml:"amount"`
}

// Dynamic leader scheduler configuration
type DynamicValidatorConfig struct {
	Pubkey      string `yaml:"pubkey"`
	Stake       uint64 `yaml:"stake"`
	ActiveStake uint64 `yaml:"active_stake"`
	IsActive    bool   `yaml:"is_active"`
}

type DynamicLeaderSchedulerConfig struct {
	Enabled         bool                     `yaml:"enabled"`
	SlotsPerEpoch   uint64                   `yaml:"slots_per_epoch"`
	Validators      []DynamicValidatorConfig `yaml:"validators"`
	TotalStake      uint64                   `yaml:"total_stake"`
	VotingThreshold float64                  `yaml:"voting_threshold"`
}

// GenesisConfig holds the configuration from genesis.yml
type GenesisConfig struct {
	DynamicLeaderScheduler *DynamicLeaderSchedulerConfig `yaml:"dynamic_leader_scheduler,omitempty"`
	LeaderSchedule         []LeaderSchedule              `yaml:"leader_schedule"`
	Alloc                  Alloc                         `yaml:"alloc"`
	Poh                    PohConfig                     `yaml:"poh"`
	Mempool                MempoolConfig                 `yaml:"mempool"`
	Validator              ValidatorConfig               `yaml:"validator"`
}

// ConfigFile is the top-level structure for genesis.yml
type ConfigFile struct {
	Config GenesisConfig `yaml:"config"`
}
