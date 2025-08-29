package db

import (
	"fmt"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
)

// LevelDBProvider implements DatabaseProvider for LevelDB
type LevelDBProvider struct {
	once sync.Once
	db   *leveldb.DB
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

// Delete removes a key-value pair
func (p *LevelDBProvider) Delete(key []byte) error {
	return p.db.Delete(key, nil)
}

// Has checks if a key exists
func (p *LevelDBProvider) Has(key []byte) (bool, error) {
	return p.db.Has(key, nil)
}

// Close closes the database connection
func (p *LevelDBProvider) Close() error {
	// avoid double close when being used for multiple store
	var err error
	p.once.Do(func() {
		err = p.db.Close()
	})
	return err
}

// Batch returns a new batch for atomic operations
func (p *LevelDBProvider) Batch() DatabaseBatch {
	return &LevelDBBatch{
		batch: new(leveldb.Batch),
		db:    p.db,
	}
}

// LevelDBBatch implements DatabaseBatch for LevelDB
type LevelDBBatch struct {
	batch *leveldb.Batch
	db    *leveldb.DB
}

// Put adds a key-value pair to the batch
func (b *LevelDBBatch) Put(key, value []byte) {
	b.batch.Put(key, value)
}

// Delete adds a deletion to the batch
func (b *LevelDBBatch) Delete(key []byte) {
	b.batch.Delete(key)
}

// Write commits all operations in the batch
func (b *LevelDBBatch) Write() error {
	return b.db.Write(b.batch, nil)
}

// Reset clears the batch
func (b *LevelDBBatch) Reset() {
	b.batch.Reset()
}

// Close releases batch resources
func (b *LevelDBBatch) Close() error {
	// LevelDB batch doesn't need explicit closing
	return nil
}
