package blockstore

import (
	"fmt"
)

// StoreType represents the type of blockstore implementation
type StoreType string

const (
	// LevelDBStoreType uses the LevelDB implementation
	LevelDBStoreType StoreType = "leveldb"
	// RocksDBStoreType uses the RocksDB implementation
	RocksDBStoreType StoreType = "rocksdb"
)

// StoreConfig holds configuration for creating blockstore instances
type StoreConfig struct {
	// Type specifies which store implementation to use
	Type StoreType `json:"type" yaml:"type"`

	// Directory is the database directory path
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
	case LevelDBStoreType, RocksDBStoreType:
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

// CreateStore creates a new blockstore instance
func (sf *StoreFactory) CreateStore(config *StoreConfig, seed []byte) (Store, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	switch config.Type {
	case LevelDBStoreType:
		return NewLevelDBStore(config.Directory, seed)

	case RocksDBStoreType:
		return NewRocksDBStore(config.Directory, seed)

	default:
		return nil, fmt.Errorf("unsupported store type: %s", config.Type)
	}
}

// Global factory instance
var globalFactory = NewStoreFactory()

// CreateStore creates a new blockstore instance using the global factory
func CreateStore(config *StoreConfig, seed []byte) (Store, error) {
	return globalFactory.CreateStore(config, seed)
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
