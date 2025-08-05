package blockchain

import "time"

type MmnConfig struct {
	Endpoint string
	Timeout  time.Duration
	ChainID  string
}
