//go:build rocksdb
// +build rocksdb

package db

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/linxGnu/grocksdb"
)

// RocksDBProvider implements DatabaseProvider for RocksDB
type RocksDBProvider struct {
	once sync.Once
	db   *grocksdb.DB
	ro   *grocksdb.ReadOptions
	wo   *grocksdb.WriteOptions
}

// NewRocksDBProvider creates a new RocksDB provider
func NewRocksDBProvider(directory string) (DatabaseProvider, error) {
	opts := grocksdb.NewDefaultOptions()
	opts.SetCreateIfMissing(true)

	db, err := grocksdb.OpenDb(opts, directory)
	if err != nil {
		return nil, fmt.Errorf("failed to open RocksDB: %w", err)
	}

	return &RocksDBProvider{
		db: db,
		ro: grocksdb.NewDefaultReadOptions(),
		wo: grocksdb.NewDefaultWriteOptions(),
	}, nil
}

func NewOptimizedRocksDBProvider(directory string) (DatabaseProvider, error) {
	opts := grocksdb.NewDefaultOptions()
	opts.SetCreateIfMissing(true)

	// Performance tuning for blockchain workload
	opts.SetMaxBackgroundCompactions(4)
	opts.SetMaxBackgroundFlushes(2)
	// Memory optimization
	opts.SetWriteBufferSize(64 * 1024 * 1024) // 64MB write buffer

	// Block-based table options for cache
	bbto := grocksdb.NewDefaultBlockBasedTableOptions()
	blockCache := grocksdb.NewLRUCache(128 * 1024 * 1024) // 128MB cache
	bbto.SetBlockCache(blockCache)

	opts.SetBlockBasedTableFactory(bbto)

	// Compression
	opts.SetCompression(grocksdb.SnappyCompression)
	// Read optimization
	opts.SetMaxOpenFiles(1000)

	db, err := grocksdb.OpenDb(opts, directory)
	if err != nil {
		return nil, fmt.Errorf("failed to open RocksDB: %w", err)
	}

	return &RocksDBProvider{
		db: db,
		ro: grocksdb.NewDefaultReadOptions(),
		wo: grocksdb.NewDefaultWriteOptions(),
	}, nil
}

// Get retrieves a value by key
func (p *RocksDBProvider) Get(key []byte) ([]byte, error) {
	value, err := p.db.Get(p.ro, key)
	if err != nil {
		return nil, err
	}
	defer value.Free()

	if !value.Exists() {
		return nil, nil // Return nil for not found, consistent with interface
	}

	// Copy the data since we're freeing the slice
	data := value.Data()
	result := make([]byte, len(data))
	copy(result, data)
	return result, nil
}

// GetBatch retrieves multiple values by keys in a single operation
func (p *RocksDBProvider) GetBatch(keys [][]byte) (map[string][]byte, error) {
	if len(keys) == 0 {
		return make(map[string][]byte), nil
	}

	// Use RocksDB MultiGet for efficient batch reads
	values, err := p.db.MultiGet(p.ro, keys...)
	if err != nil {
		return nil, err
	}

	result := make(map[string][]byte, len(keys))

	for i, value := range values {
		keyStr := string(keys[i])

		if value.Data() != nil {
			// Copy the data since we're freeing the slice
			data := value.Data()
			result[keyStr] = make([]byte, len(data))
			copy(result[keyStr], data)
		}
		// If value.Data() is nil, the key doesn't exist - don't add to result

		value.Free() // Free each value
	}

	return result, nil
}

// Put stores a key-value pair
func (p *RocksDBProvider) Put(key, value []byte) error {
	return p.db.Put(p.wo, key, value)
}

// Delete removes a key-value pair
func (p *RocksDBProvider) Delete(key []byte) error {
	return p.db.Delete(p.wo, key)
}

// Has checks if a key exists
func (p *RocksDBProvider) Has(key []byte) (bool, error) {
	// Only check key existence, not value
	value, err := p.db.Get(p.ro, key)
	if err != nil {
		return false, err
	}
	defer value.Free()

	// Check if the key actually exists (not just a tombstone)
	return value.Exists(), nil
}

// Close closes the database connection
func (p *RocksDBProvider) Close() error {
	// avoid double close when being used for multiple store
	p.once.Do(func() {
		p.ro.Destroy()
		p.wo.Destroy()
		p.db.Close()
	})
	return nil
}

// Batch creates a new batch for atomic operations
func (p *RocksDBProvider) Batch() DatabaseBatch {
	return &RocksDBBatch{
		batch:    grocksdb.NewWriteBatch(),
		provider: p,
	}
}

// IteratePrefix iterates over all key-value pairs with the given prefix
func (p *RocksDBProvider) IteratePrefix(prefix []byte, callback func(key, value []byte) bool) error {
	iter := p.db.NewIterator(p.ro)
	defer iter.Close()

	// Seek to the prefix
	iter.Seek(prefix)

	// Iterate through all keys with the prefix
	for iter.Valid() {
		keySlice := iter.Key()
		valueSlice := iter.Value()

		// Convert grocksdb.Slice to []byte
		key := keySlice.Data()
		value := valueSlice.Data()

		// Check if key still starts with prefix
		if len(key) < len(prefix) || !bytes.HasPrefix(key, prefix) {
			break
		}

		// Call callback function
		if !callback(key, value) {
			break
		}

		iter.Next()
	}

	return iter.Err()
}

// RocksDBBatch implements DatabaseBatch for RocksDB
type RocksDBBatch struct {
	batch    *grocksdb.WriteBatch
	provider *RocksDBProvider
}

// Put adds a key-value pair to the batch
func (b *RocksDBBatch) Put(key, value []byte) {
	b.batch.Put(key, value)
}

// Delete adds a deletion to the batch
func (b *RocksDBBatch) Delete(key []byte) {
	b.batch.Delete(key)
}

// Write commits all operations in the batch
func (b *RocksDBBatch) Write() error {
	return b.provider.db.Write(b.provider.wo, b.batch)
}

// Reset clears the batch
func (b *RocksDBBatch) Reset() {
	b.batch.Clear()
}

// Close releases batch resources
func (b *RocksDBBatch) Close() error {
	b.batch.Destroy()
	return nil
}
