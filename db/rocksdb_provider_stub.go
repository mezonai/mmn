//go:build !rocksdb
// +build !rocksdb

package db

import "fmt"

// NewRocksDBProvider creates a stub that returns an error when rocksdb is not available
func NewRocksDBProvider(directory string) (DatabaseProvider, error) {
	return nil, fmt.Errorf("RocksDB support not compiled in. Build with -tags rocksdb to enable RocksDB support")
}

func NewOptimizedRocksDBProvider(directory string) (DatabaseProvider, error) {
	return nil, fmt.Errorf("RocksDB support not compiled in. Build with -tags rocksdb to enable RocksDB support")
}
