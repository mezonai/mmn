package store

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/types"
)

// TxMetaStore is the interface for transaction meta store
// that is responsible for persisting operations of transaction metadata
type TxMetaStore interface {
	Store(txMeta *types.TransactionMeta) error
	StoreBatch(txMetas []*types.TransactionMeta) error
	GetByHash(txHash string) (*types.TransactionMeta, error)
	GetBatch(txHashes []string) (map[string]*types.TransactionMeta, error)
	StoreToBatch(batch db.DatabaseBatch, txMetas []*types.TransactionMeta) error
	MustClose()
}

// GenericTxMetaStore provides transaction meta storage operations
type GenericTxMetaStore struct {
	mu         sync.RWMutex
	dbProvider db.DatabaseProvider
}

// NewGenericTxMetaStore creates a new transaction meta store
func NewGenericTxMetaStore(dbProvider db.DatabaseProvider) (*GenericTxMetaStore, error) {
	if dbProvider == nil {
		return nil, fmt.Errorf("provider cannot be nil")
	}

	return &GenericTxMetaStore{
		dbProvider: dbProvider,
	}, nil
}

// Store stores a transaction meta in the database
func (tms *GenericTxMetaStore) Store(txMeta *types.TransactionMeta) error {
	return tms.StoreBatch([]*types.TransactionMeta{txMeta})
}

// StoreBatch stores a batch of transaction metas in the database
func (tms *GenericTxMetaStore) StoreBatch(txMetas []*types.TransactionMeta) error {
	tms.mu.Lock()
	defer tms.mu.Unlock()

	batch := tms.dbProvider.Batch()
	for _, txMeta := range txMetas {
		data, err := json.Marshal(txMeta)
		if err != nil {
			return fmt.Errorf("failed to marshal transaction meta: %w", err)
		}

		batch.Put(tms.getDbKey(txMeta.TxHash), data)
	}

	err := batch.Write()
	if err != nil {
		return fmt.Errorf("failed to write transaction meta to database: %w", err)
	}

	return nil
}

// GetByHash retrieves a transaction meta by its transaction hash
func (tms *GenericTxMetaStore) GetByHash(txHash string) (*types.TransactionMeta, error) {
	tms.mu.RLock()
	defer tms.mu.RUnlock()

	data, err := tms.dbProvider.Get(tms.getDbKey(txHash))
	if err != nil {
		return nil, fmt.Errorf("could not get transaction meta %s from db: %w", txHash, err)
	}

	var txMeta types.TransactionMeta
	err = json.Unmarshal(data, &txMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction meta %s: %w", txHash, err)
	}

	return &txMeta, nil
}

// GetBatch retrieves multiple transaction metas by their hashes
func (tms *GenericTxMetaStore) GetBatch(txHashes []string) (map[string]*types.TransactionMeta, error) {
	tms.mu.RLock()
	defer tms.mu.RUnlock()

	if len(txHashes) == 0 {
		return map[string]*types.TransactionMeta{}, nil
	}

	txMetas := make(map[string]*types.TransactionMeta, len(txHashes))
	for _, txHash := range txHashes {
		tm, err := tms.GetByHash(txHash)
		if err != nil {
			log.Printf("Could not get transaction meta %s from database: %s", txHash, err.Error())
			continue
		}
		txMetas[txHash] = tm
	}

	return txMetas, nil
}

// StoreToBatch stores a batch of transaction metas in the database using an existing batch
func (tms *GenericTxMetaStore) StoreToBatch(batch db.DatabaseBatch, txMetas []*types.TransactionMeta) error {
	for _, txMeta := range txMetas {
		data, err := json.Marshal(txMeta)
		if err != nil {
			return fmt.Errorf("failed to marshal transaction meta: %w", err)
		}
		batch.Put(tms.getDbKey(txMeta.TxHash), data)
	}

	return nil
}

// MustClose closes the transaction meta store and related resources
func (tms *GenericTxMetaStore) MustClose() {
	err := tms.dbProvider.Close()
	if err != nil {
		logx.Error("TX_META_STORE", "Failed to close provider")
	}
}

func (tms *GenericTxMetaStore) getDbKey(txHash string) []byte {
	return []byte(PrefixTxMeta + txHash)
}
