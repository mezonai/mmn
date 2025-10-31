package store

import (
	"fmt"

	"github.com/mezonai/mmn/db"

	"github.com/mezonai/mmn/events"
)

// StoreType represents the type of store implementation
type StoreType string

const (
	// LevelDBStoreType uses the LevelDB implementation
	LevelDBStoreType StoreType = "leveldb"

	// RocksDBStoreType uses the RocksDB implementation
	RocksDBStoreType StoreType = "rocksdb"

	// RedisStoreType uses the Redis implementation
	RedisStoreType StoreType = "redis"
)

// StoreConfig holds configuration for creating store instances
type StoreConfig struct {
	// Type specifies which store implementation to use
	Type StoreType `json:"type" yaml:"type"`

	// Directory is the database directory path (for file-based databases)
	Directory string `json:"directory" yaml:"directory"`
}

// Validate validates the store configuration
func (sc *StoreConfig) Validate() error {
	if sc.Type == "" {
		return fmt.Errorf("store type cannot be empty")
	}

	if sc.Directory == "" {
		return fmt.Errorf("directory cannot be empty")
	}

	switch sc.Type {
	case LevelDBStoreType, RocksDBStoreType, RedisStoreType:
		return nil
	default:
		return fmt.Errorf("unsupported store type: %s", sc.Type)
	}
}

// StoreFactory take responsibility to create store instances
type StoreFactory struct{}

// NewStoreFactory creates a new store factory
func NewStoreFactory() *StoreFactory {
	return &StoreFactory{}
}

// CreateStoreWithProvider creates store instances using the provider pattern
func (sf *StoreFactory) CreateStoreWithProvider(config *StoreConfig, eventRouter *events.EventRouter) (AccountStore, TxStore, TxMetaStore, BlockStore, MultisigFaucetStore, error) {
	if config == nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("config cannot be nil")
	}

	if err := config.Validate(); err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create the appropriate provider
	provider, err := sf.CreateProvider(config)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to create provider: %w", err)
	}

	accStore, err := NewGenericAccountStore(provider)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to create account store: %w", err)
	}

	txStore, err := NewGenericTxStore(provider)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to create transaction store: %w", err)
	}

	txMetaStore, err := NewGenericTxMetaStore(provider)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to create transaction meta store: %w", err)
	}

	blkStore, err := NewGenericBlockStore(provider, txStore, txMetaStore, eventRouter)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to create block store: %w", err)
	}

	multisigFaucetStore, err := NewGenericMultisigFaucetStore(provider)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to create multisig faucet store: %w", err)
	}


	return accStore, txStore, txMetaStore, blkStore, multisigFaucetStore, nil
}

// CreateProvider creates a database provider based on the configuration
func (sf *StoreFactory) CreateProvider(config *StoreConfig) (db.DatabaseProvider, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	switch config.Type {
	case LevelDBStoreType:
		return db.NewLevelDBProvider(config.Directory)

	case RocksDBStoreType:
		return db.NewOptimizedRocksDBProvider(config.Directory)

	case RedisStoreType:
		// just for debug
		return db.NewRedisProvider("localhost:6379")

	default:
		return nil, fmt.Errorf("unsupported store type: %s", config.Type)
	}
}

// Global factory instance
var globalFactory = NewStoreFactory()

// CreateStore creates new store instances using the global factory
func CreateStore(config *StoreConfig, eventRouter *events.EventRouter) (AccountStore, TxStore, TxMetaStore, BlockStore, MultisigFaucetStore, error) {
	return globalFactory.CreateStoreWithProvider(config, eventRouter)
}
