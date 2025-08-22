package blockstore

import (
	"fmt"
	"github.com/mezonai/mmn/db"
)

type DBVendor string

const (
	LevelDB DBVendor = "leveldb"
	RocksDB DBVendor = "rocksdb"
	Redis   DBVendor = "redis" // For debug
)

type DBOptions struct {
	Directory string
}

// Deprecated
func CreateDBProvider(vendor DBVendor, options DBOptions) (db.DatabaseProvider, error) {
	switch vendor {
	case LevelDB:
		return db.NewLevelDBProvider(options.Directory)

	case RocksDB:
		return db.NewRocksDBProvider(options.Directory)

	case Redis:
		return db.NewRedisProvider("localhost:6379")

	default:
		return nil, fmt.Errorf("unsupported db provider: %s", vendor)
	}
}
