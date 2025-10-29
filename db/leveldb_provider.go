package db

import (
	"bytes"
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

// GetBatch retrieves multiple values by keys in a single operation
func (p *LevelDBProvider) GetBatch(keys [][]byte) (map[string][]byte, error) {
	if len(keys) == 0 {
		return make(map[string][]byte), nil
	}

	result := make(map[string][]byte, len(keys))

	// LevelDB doesn't have native MultiGet, so we use individual gets
	// but at least we batch them in a single function call
	for _, key := range keys {
		value, err := p.db.Get(key, nil)
		if err != nil {
			if err != leveldb.ErrNotFound {
				return nil, err // Return error for non-NotFound errors
			}
			// Skip not found keys - don't add to result
			continue
		}

		keyStr := string(key)
		result[keyStr] = value
	}

	return result, nil
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

// IteratePrefix iterates over all key-value pairs with the given prefix
func (p *LevelDBProvider) IteratePrefix(prefix []byte, callback func(key, value []byte) bool) error {
	iter := p.db.NewIterator(nil, nil)
	defer iter.Release()

	// Seek to the prefix
	iter.Seek(prefix)

	// Iterate through all keys with the prefix
	for iter.Valid() {
		key := iter.Key()

		// Check if key still starts with prefix
		if len(key) < len(prefix) || !bytes.HasPrefix(key, prefix) {
			break
		}

		// Call callback function
		if !callback(key, iter.Value()) {
			break
		}

		iter.Next()
	}

	return iter.Error()
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
func (b *LevelDBBatch) Close() {
	// LevelDB batch doesn't need explicit closing
}
