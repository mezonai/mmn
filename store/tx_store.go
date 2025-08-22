package store

import (
	"encoding/json"
	"fmt"
	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/logx"
	"log"
	"sync"

	"github.com/mezonai/mmn/types"
)

// TxStore is the interface for transaction store that is responsible for persisting operations of tx
type TxStore interface {
	Store(tx *types.Transaction) error
	StoreBatch(txs []*types.Transaction) error
	GetByHash(txHash string) (*types.Transaction, error)
	GetBatch(txHashes []string) ([]*types.Transaction, error)
	MustClose()
}

// GenericTxStore provides transaction storage operations
type GenericTxStore struct {
	mu         sync.RWMutex
	dbProvider db.DatabaseProvider
}

// NewGenericTxStore creates a new transaction store
func NewGenericTxStore(dbProvider db.DatabaseProvider) (*GenericTxStore, error) {
	if dbProvider == nil {
		return nil, fmt.Errorf("provider cannot be nil")
	}

	return &GenericTxStore{
		dbProvider: dbProvider,
	}, nil
}

// Store stores a transaction in the database
func (ts *GenericTxStore) Store(tx *types.Transaction) error {
	return ts.StoreBatch([]*types.Transaction{tx})
}

// StoreBatch stores a batch of transactions in the database
func (ts *GenericTxStore) StoreBatch(txs []*types.Transaction) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	batch := ts.dbProvider.Batch()
	for _, tx := range txs {
		txData, err := json.Marshal(tx)
		if err != nil {
			return fmt.Errorf("failed to marshal transaction: %w", err)
		}

		batch.Put(ts.getDbKey(tx.Hash()), txData)
	}

	err := batch.Write()
	if err != nil {
		return fmt.Errorf("failed to write transaction to database: %w", err)
	}

	return nil
}

// GetByHash retrieves a transaction by its hash
func (ts *GenericTxStore) GetByHash(txHash string) (*types.Transaction, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	data, err := ts.dbProvider.Get(ts.getDbKey(txHash))
	if err != nil {
		return nil, fmt.Errorf("could not get transaction %s from db: %w", txHash, err)
	}

	// Deserialize transaction
	var tx types.Transaction
	err = json.Unmarshal(data, &tx)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction %s: %w", txHash, err)
	}

	return &tx, nil
}

// GetBatch retrieves multiple transactions by their hashes
func (ts *GenericTxStore) GetBatch(txHashes []string) ([]*types.Transaction, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if len(txHashes) == 0 {
		return []*types.Transaction{}, nil
	}

	// TODO: implement batch get
	transactions := make([]*types.Transaction, 0, len(txHashes))
	for _, txHash := range txHashes {
		t, err := ts.GetByHash(txHash)
		if err != nil {
			log.Printf("Could not get transaction %s from database: %s", txHash, err.Error())
			continue
		}
		transactions = append(transactions, t)
	}

	return transactions, nil
}

// MustClose closes the transaction store and related resources
func (ts *GenericTxStore) MustClose() {
	err := ts.dbProvider.Close()
	if err != nil {
		logx.Error("TX_STORE", "Failed to close provider")
	}
}

func (ts *GenericTxStore) getDbKey(txHash string) []byte {
	return []byte(PrefixTx + txHash)
}
