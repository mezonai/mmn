package blockstore

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/mezonai/mmn/types"
)

const (
	txMetaPrefix = "txmeta:"
)

type TxMetaStore interface {
	Store(txMeta *types.TransactionMeta) error
	StoreBatch(txMetas []*types.TransactionMeta) error
	GetByHash(txHash string) (*types.TransactionMeta, error)
	GetBatch(txHashes []string) (map[string]*types.TransactionMeta, error)
	Close() error
}

type GenericTxMetaStore struct {
	mu         sync.RWMutex
	dbProvider DatabaseProvider
}

// NewGenericTxMetaStore - creates a new transaction meta store
func NewGenericTxMetaStore(dbProvider DatabaseProvider) (*GenericTxMetaStore, error) {
	if dbProvider == nil {
		return nil, fmt.Errorf("provider cannot be nil")
	}

	return &GenericTxMetaStore{
		dbProvider: dbProvider,
	}, nil
}

// Store - store a transaction meta in the database
func (tms *GenericTxMetaStore) Store(txMeta *types.TransactionMeta) error {
	return tms.StoreBatch([]*types.TransactionMeta{txMeta})
}

// StoreBatch - store a batch of transaction metas in the database
func (tms *GenericTxMetaStore) StoreBatch(txMetas []*types.TransactionMeta) error {
	tms.mu.Lock()
	defer tms.mu.Unlock()

	batch := tms.dbProvider.Batch()
	for _, txMeta := range txMetas {
		txMetaKey := []byte(txMetaPrefix + txMeta.TxHash)
		txMetaData, err := json.Marshal(txMeta)
		if err != nil {
			return fmt.Errorf("failed to marshal transaction meta: %w", err)
		}

		batch.Put(txMetaKey, txMetaData)
	}

	err := batch.Write()
	if err != nil {
		return fmt.Errorf("failed to write transaction meta to database: %w", err)
	}

	return nil
}

// GetByHash - retrieves a transaction meta by transaction hash
func (tms *GenericTxMetaStore) GetByHash(txHash string) (*types.TransactionMeta, error) {
	tms.mu.RLock()
	defer tms.mu.RUnlock()

	data, err := tms.dbProvider.Get([]byte(txMetaPrefix + txHash))
	if err != nil {
		return nil, fmt.Errorf("could not get transaction meta %s from db: %w", txHash, err)
	}

	var txMeta types.TransactionMeta
	err = json.Unmarshal(data, &txMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction meta: %w", err)
	}

	return &txMeta, nil
}

// GetBatch - retrieves a batch of transaction metas by their hashes
func (tms *GenericTxMetaStore) GetBatch(txHashes []string) (map[string]*types.TransactionMeta, error) {
	tms.mu.RLock()
	defer tms.mu.RUnlock()

	if len(txHashes) == 0 {
		return map[string]*types.TransactionMeta{}, nil
	}

	txMetas := make(map[string]*types.TransactionMeta)
	for _, txHash := range txHashes {
		txMeta, err := tms.GetByHash(txHash)
		if err != nil {
			log.Printf("Could not get transaction meta for hash %s: %s", txHash, err.Error())
			continue
		}
		txMetas[txHash] = txMeta
	}

	return txMetas, nil
}

// Close - closes the transaction meta store
func (tms *GenericTxMetaStore) Close() error {
	return tms.dbProvider.Close()
}
