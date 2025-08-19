package blockstore

// DatabaseProvider abstracts the low-level database operations
// This interface allows BlockStore to work with different database backends
// without knowing the specific implementation details
type DatabaseProvider interface {
	// Get retrieves a value by key
	Get(key []byte) ([]byte, error)
	
	// Put stores a key-value pair
	Put(key, value []byte) error
	
	// Delete removes a key-value pair
	Delete(key []byte) error
	
	// Has checks if a key exists
	Has(key []byte) (bool, error)
	
	// Close closes the database connection
	Close() error
	
	// Batch returns a new batch for atomic operations
	Batch() DatabaseBatch
}

// DatabaseBatch provides atomic batch operations
type DatabaseBatch interface {
	// Put adds a key-value pair to the batch
	Put(key, value []byte)
	
	// Delete adds a deletion to the batch
	Delete(key []byte)
	
	// Write commits all operations in the batch
	Write() error
	
	// Reset clears the batch
	Reset()
	
	// Close releases batch resources
	Close() error
}