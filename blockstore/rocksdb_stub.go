//go:build !rocksdb
// +build !rocksdb

package blockstore

import "fmt"

// NewRocksDBStore returns an error when RocksDB is not supported
func NewRocksDBStore(dir string, seed []byte) (Store, error) {
	return nil, fmt.Errorf("RocksDB support not compiled in. Use -tags rocksdb to enable")
}

// IsRocksDBSupported returns false when RocksDB is not compiled in
func IsRocksDBSupported() bool {
	return false
}