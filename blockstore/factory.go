package blockstore

import (
	"fmt"
	"github.com/mezonai/mmn/db"
)

// StoreType represents the type of blockstore implementation
type StoreType string

const (
	// LevelDBStoreType uses the LevelDB implementation
	LevelDBStoreType StoreType = "leveldb"
	// RocksDBStoreType uses the RocksDB implementation
	RocksDBStoreType StoreType = "rocksdb"
	// RedisStoreType uses the Redis implementation
	RedisStoreType StoreType = "redis"
)

// StoreConfig holds configuration for creating blockstore instances
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

// StoreFactory creates blockstore instances
type StoreFactory struct{}

// NewStoreFactory creates a new store factory
func NewStoreFactory() *StoreFactory {
	return &StoreFactory{}
}

// CreateStore creates a new blockstore instance using the legacy approach
// Deprecated: Use CreateStoreWithProvider for better abstraction
func (sf *StoreFactory) CreateStore(config *StoreConfig) (Store, error) {
	// Redirect to the provider-based implementation for consistency
	return sf.CreateStoreWithProvider(config, nil)
}

// CreateStoreWithProvider creates a new blockstore instance using the provider pattern
func (sf *StoreFactory) CreateStoreWithProvider(config *StoreConfig, ts TxStore) (Store, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create the appropriate provider
	provider, err := sf.CreateProvider(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider: %w", err)
	}

	// Create generic blockstore with the provider
	return NewGenericBlockStore(provider, ts)
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
		return db.NewRocksDBProvider(config.Directory)

	case RedisStoreType:
		// just for debug
		return db.NewRedisProvider("localhost:6379")

	default:
		return nil, fmt.Errorf("unsupported store type: %s", config.Type)
	}
}

// Global factory instance
var globalFactory = NewStoreFactory()

// CreateStore creates a new blockstore instance using the global factory
func CreateStore(config *StoreConfig, ts TxStore) (Store, error) {
	return globalFactory.CreateStoreWithProvider(config, ts)
}

// NewLevelDBConfig creates a LevelDB store configuration
func NewLevelDBConfig(directory string) *StoreConfig {
	return &StoreConfig{
		Type:      LevelDBStoreType,
		Directory: directory,
	}
}

// NewRocksDBConfig creates a RocksDB store configuration
func NewRocksDBConfig(directory string) *StoreConfig {
	return &StoreConfig{
		Type:      RocksDBStoreType,
		Directory: directory,
	}
}
