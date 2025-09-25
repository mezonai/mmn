package store

import (
	"fmt"

	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/jsonx"
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
	MustClose()
}

// GenericTxMetaStore provides transaction meta storage operations
type GenericTxMetaStore struct {
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
	if len(txMetas) == 0 {
		logx.Info("TX_META_STORE", "StoreBatch: no transaction metas to store")
		return nil
	}
	logx.Info("TX_META_STORE", fmt.Sprintf("StoreBatch: storing %d transaction metas", len(txMetas)))

	batch := tms.dbProvider.Batch()
	defer batch.Close()

	for _, txMeta := range txMetas {
		data, err := jsonx.Marshal(txMeta)
		if err != nil {
			return fmt.Errorf("failed to marshal transaction meta: %w", err)
		}

		batch.Put(tms.getDbKey(txMeta.TxHash), data)
	}

	err := batch.Write()
	if err != nil {
		return fmt.Errorf("failed to write transaction meta to database: %w", err)
	}

	logx.Info("TX_META_STORE", fmt.Sprintf("StoreBatch: stored %d transaction metas", len(txMetas)))
	return nil
}

// GetByHash retrieves a transaction meta by its transaction hash
func (tms *GenericTxMetaStore) GetByHash(txHash string) (*types.TransactionMeta, error) {
	data, err := tms.dbProvider.Get(tms.getDbKey(txHash))
	if err != nil {
		return nil, fmt.Errorf("could not get transaction meta %s from db: %w", txHash, err)
	}

	var txMeta types.TransactionMeta
	err = jsonx.Unmarshal(data, &txMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction meta %s: %w", txHash, err)
	}

	return &txMeta, nil
}

// GetBatch retrieves multiple transaction metas by their hashes
func (tms *GenericTxMetaStore) GetBatch(txHashes []string) (map[string]*types.TransactionMeta, error) {
	if len(txHashes) == 0 {
		return map[string]*types.TransactionMeta{}, nil
	}

	txMetas := make(map[string]*types.TransactionMeta, len(txHashes))
	for _, txHash := range txHashes {
		data, err := tms.dbProvider.Get(tms.getDbKey(txHash))
		if err != nil {
			logx.Warn("TX_META_STORE", fmt.Sprintf("Could not get transaction meta %s from database: %s", txHash, err.Error()))
			continue
		}

		var txMeta types.TransactionMeta
		err = jsonx.Unmarshal(data, &txMeta)
		if err != nil {
			logx.Warn("TX_META_STORE", fmt.Sprintf("Failed to unmarshal transaction meta %s: %s", txHash, err.Error()))
			continue
		}

		txMetas[txHash] = &txMeta
	}

	return txMetas, nil
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
