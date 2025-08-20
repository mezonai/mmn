package blockstore

import "fmt"

type DBVendor string

const (
	LevelDB DBVendor = "leveldb"
	RocksDB DBVendor = "rocksdb"
	Redis   DBVendor = "redis" // For debug
)

type DBOptions struct {
	Directory string
}

func CreateDBProvider(vendor DBVendor, options DBOptions) (DatabaseProvider, error) {
	switch vendor {
	case LevelDB:
		return NewLevelDBProvider(options.Directory)

	case RocksDB:
		return NewRocksDBProvider(options.Directory)

	case Redis:
		return NewRedisProvider("localhost:6379")

	default:
		return nil, fmt.Errorf("unsupported db provider: %s", vendor)
	}
}
