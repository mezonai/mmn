package db

import (
	"fmt"

	"github.com/mezonai/mmn/logx"
)

// DBTxManager manages database transactions (batches) for atomic operations
// across multiple stores. It uses the shared DatabaseProvider.
type DBTxManager struct {
	provider DatabaseProvider
}

// NewDBTxManager creates a new transaction manager with the given provider
func NewDBTxManager(provider DatabaseProvider) *DBTxManager {
	return &DBTxManager{provider: provider}
}

// WithBatch executes the given function within a batch context.
// If the function returns nil, the batch is committed; otherwise, it's discarded.
func (tm *DBTxManager) WithBatch(fn func(batch DatabaseBatch) error) error {
	batch := tm.provider.Batch()
	defer func() {
		if err := batch.Close(); err != nil {
			logx.Error("TX_MANAGER", "Failed to close batch:", err)
		}
	}()

	if err := fn(batch); err != nil {
		batch.Reset()
		return fmt.Errorf("transaction failed: %w", err)
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	return nil
}
