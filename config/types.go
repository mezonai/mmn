package config

import (
	"fmt"

	"github.com/holiman/uint256"
	"gopkg.in/yaml.v3"
)

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
	Address string      `yaml:"address"`
	Amount  *uint256.Int `yaml:"amount"`
}

// Custom YAML marshaling for uint256.Int Amount field
func (a *Address) MarshalYAML() (interface{}, error) {
	amountStr := "0"
	if a.Amount != nil {
		amountStr = a.Amount.String()
	}
	
	return map[string]interface{}{
		"address": a.Address,
		"amount":  amountStr,
	}, nil
}

func (a *Address) UnmarshalYAML(value *yaml.Node) error {
	var aux struct {
		Address string `yaml:"address"`
		Amount  string `yaml:"amount"`
	}
	
	if err := value.Decode(&aux); err != nil {
		return err
	}
	
	a.Address = aux.Address
	
	// Parse amount
	if aux.Amount == "" {
		a.Amount = uint256.NewInt(0)
	} else {
		// Try to parse as decimal first
		amount, err := uint256.FromDecimal(aux.Amount)
		if err != nil {
			// If decimal parsing fails, try as hex
			if len(aux.Amount) >= 2 && (aux.Amount[:2] == "0x" || aux.Amount[:2] == "0X") {
				amount, err = uint256.FromHex(aux.Amount)
				if err != nil {
					return fmt.Errorf("invalid amount format: %w", err)
				}
			} else {
				return fmt.Errorf("invalid amount format: %w", err)
			}
		}
		a.Amount = amount
	}
	
	return nil
}

// GenesisConfig holds the configuration from genesis.yml
type GenesisConfig struct {
	LeaderSchedule []LeaderSchedule `yaml:"leader_schedule"`
	Alloc          Alloc            `yaml:"alloc"`
	Poh            PohConfig        `yaml:"poh"`
	Mempool        MempoolConfig    `yaml:"mempool"`
	Validator      ValidatorConfig  `yaml:"validator"`
}

// ConfigFile is the top-level structure for genesis.yml
type ConfigFile struct {
	Config GenesisConfig `yaml:"config"`
}
