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

// StakingConfig holds staking-related configuration
type StakingConfig struct {
	Enabled           bool   `yaml:"enabled"`
	SlotsPerEpoch     uint64 `yaml:"slots_per_epoch"`
	MinStakeAmount    string `yaml:"min_stake_amount"`    // String to handle big numbers
	MaxValidators     int    `yaml:"max_validators"`
	EpochRewardAmount string `yaml:"epoch_reward_amount"` // String to handle big numbers
	UnstakeCooldown   uint64 `yaml:"unstake_cooldown"`    // slots
	ActivationDelay   uint64 `yaml:"activation_delay"`    // slots
}

// GenesisValidator represents a validator in genesis
type GenesisValidator struct {
	Pubkey      string `yaml:"pubkey"`
	StakeAmount string `yaml:"stake_amount"` // String to handle big numbers
	Commission  uint8  `yaml:"commission"`   // 0-100%
}

// GenesisConfig holds the configuration from genesis.yml
type GenesisConfig struct {
	LeaderSchedule    []LeaderSchedule   `yaml:"leader_schedule"`
	Faucet            Faucet             `yaml:"faucet"`
	Poh               PohConfig          `yaml:"poh"`
	Mempool           MempoolConfig      `yaml:"mempool"`
	Validator         ValidatorConfig    `yaml:"validator"`
	Staking           StakingConfig      `yaml:"staking"`
	GenesisValidators []GenesisValidator `yaml:"genesis_validators"`
}

// ConfigFile is the top-level structure for genesis.yml
type ConfigFile struct {
	Config GenesisConfig `yaml:"config"`
}
