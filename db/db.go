package db

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"

	"github.com/linxGnu/grocksdb"
)

const (
	CfDefault = "default"
	CfBlocks  = "blocks"
	CfTxHash  = "tx_hash" // transactions by hash
)

type RocksDB struct {
	DB           *grocksdb.DB
	CfHandlesMap map[string]*grocksdb.ColumnFamilyHandle
}

func NewRocksDB(dir string) (*RocksDB, error) {
	if dir == "" {
		return nil, errors.New("directory path cannot be empty")
	}

	opts := grocksdb.NewDefaultOptions()
	opts.SetCreateIfMissing(true)
	opts.SetCreateIfMissingColumnFamilies(true)

	cfNames := []string{CfDefault, CfBlocks, CfTxHash}
	cfOpts := []*grocksdb.Options{opts, opts, opts}

	db, cfHandles, err := grocksdb.OpenDbColumnFamilies(opts, filepath.Clean(dir), cfNames, cfOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to open RocksDB at %s: %w", dir, err)
	}

	// Create a map for easy column family lookup
	cfHandlesMap := make(map[string]*grocksdb.ColumnFamilyHandle)
	for i, name := range cfNames {
		cfHandlesMap[name] = cfHandles[i]
	}

	return &RocksDB{
		DB:           db,
		CfHandlesMap: cfHandlesMap,
	}, nil
}

// MustGetColumnFamily returns a column family handle by name
func (r *RocksDB) MustGetColumnFamily(name string) *grocksdb.ColumnFamilyHandle {
	cf, exists := r.CfHandlesMap[name]
	if !exists {
		log.Panicf("failed to find column family %s", name)
	}

	return cf
}

// Close closes the database
func (r *RocksDB) Close() {
	for name, handle := range r.CfHandlesMap {
		if handle != nil {
			handle.Destroy()
			delete(r.CfHandlesMap, name)
		}
	}

	if r.DB != nil {
		r.DB.Close()
		r.DB = nil
	}
}
