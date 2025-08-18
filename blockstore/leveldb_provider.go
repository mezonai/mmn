package blockstore

import (
	"fmt"
	"github.com/syndtr/goleveldb/leveldb"
)

// LevelDBProvider implements DatabaseProvider for LevelDB
type LevelDBProvider struct {
	db *leveldb.DB
}

// NewLevelDBProvider creates a new LevelDB provider
func NewLevelDBProvider(directory string) (DatabaseProvider, error) {
	db, err := leveldb.OpenFile(directory, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open LevelDB: %w", err)
	}
	
	return &LevelDBProvider{db: db}, nil
}

// Get retrieves a value by key
func (p *LevelDBProvider) Get(key []byte) ([]byte, error) {
	value, err := p.db.Get(key, nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			return nil, nil // Return nil for not found, consistent with interface
		}
		return nil, err
	}
	return value, nil
}

// Put stores a key-value pair
func (p *LevelDBProvider) Put(key, value []byte) error {
	return p.db.Put(key, value, nil)
}

// Delete removes a key
func (p *LevelDBProvider) Delete(key []byte) error {
	return p.db.Delete(key, nil)
}

// Has checks if a key exists
func (p *LevelDBProvider) Has(key []byte) (bool, error) {
	return p.db.Has(key, nil)
}

// Close closes the database connection
func (p *LevelDBProvider) Close() error {
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

// Batch creates a new batch for atomic operations
func (p *LevelDBProvider) Batch() DatabaseBatch {
	batch := new(leveldb.Batch)
	return &LevelDBBatch{batch: batch, db: p.db}
}

// LevelDBBatch implements DatabaseBatch for LevelDB
type LevelDBBatch struct {
	batch *leveldb.Batch
	db    *leveldb.DB
}

func (b *LevelDBBatch) Put(key, value []byte) {
	b.batch.Put(key, value)
}

func (b *LevelDBBatch) Delete(key []byte) {
	b.batch.Delete(key)
}

func (b *LevelDBBatch) Write() error {
	return b.db.Write(b.batch, nil)
}

func (b *LevelDBBatch) Reset() {
	b.batch.Reset()
}

func (b *LevelDBBatch) Close() error {
	// LevelDB batch doesn't need explicit closing
	return nil
}
