//go:build rocksdb
// +build rocksdb

package db

import (
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

	// Block-based table options for cache and bloom filter
	bbto := grocksdb.NewDefaultBlockBasedTableOptions()
	blockCache := grocksdb.NewLRUCache(128 * 1024 * 1024) // 128MB cache
	bbto.SetBlockCache(blockCache)

	// Bloom filter for KeyMayExists optimization (10 bits per key = ~1% false positive)
	bloomFilter := grocksdb.NewBloomFilter(10.0)
	bbto.SetFilterPolicy(bloomFilter)

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
	// Use KeyMayExists first (bloom filter check - very fast)
	slice := p.db.KeyMayExists(p.ro, key, "")
	if slice == nil {
		return false, nil // Definitely doesn't exist
	}

	// KeyMayExists returned something, but it might be false positive
	// Need to verify with actual Get()
	value, err := p.db.Get(p.ro, key)
	if err != nil {
		return false, err
	}

	if value == nil {
		return false, nil
	}
	defer value.Free()

	if !value.Exists() {
		return false, nil
	}

	return true, nil
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
