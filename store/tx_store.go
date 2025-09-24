package store

import (
	"fmt"
	"sync"

	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/jsonx"

	"github.com/mezonai/mmn/transaction"
)

// TxStore is the interface for transaction store that is responsible for persisting operations of tx
type TxStore interface {
	Store(tx *transaction.Transaction) error
	StoreBatch(txs []*transaction.Transaction) error
	GetByHash(txHash string) (*transaction.Transaction, error)
	GetBatch(txHashes []string) ([]*transaction.Transaction, error)
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
func (ts *GenericTxStore) Store(tx *transaction.Transaction) error {
	return ts.StoreBatch([]*transaction.Transaction{tx})
}

// StoreBatch stores a batch of transactions in the database
func (ts *GenericTxStore) StoreBatch(txs []*transaction.Transaction) error {
	if len(txs) == 0 {
		logx.Info("TX_STORE", "StoreBatch: no transactions to store")
		return nil
	}
	logx.Info("TX_STORE", "StoreBatch: storing", len(txs), "transactions")

	ts.mu.Lock()
	defer ts.mu.Unlock()

	batch := ts.dbProvider.Batch()
	defer batch.Close()

	for _, tx := range txs {
		txData, err := jsonx.Marshal(tx)
		if err != nil {
			return fmt.Errorf("failed to marshal transaction: %w", err)
		}

		batch.Put(ts.getDbKey(tx.Hash()), txData)
	}

	err := batch.Write()
	if err != nil {
		return fmt.Errorf("failed to write transaction to database: %w", err)
	}

	logx.Info("TX_STORE", "StoreBatch: stored", len(txs), "transactions")
	return nil
}

// GetByHash retrieves a transaction by its hash
func (ts *GenericTxStore) GetByHash(txHash string) (*transaction.Transaction, error) {
	data, err := ts.dbProvider.Get(ts.getDbKey(txHash))
	if err != nil {
		return nil, fmt.Errorf("could not get transaction %s from db: %w", txHash, err)
	}

	// Deserialize transaction
	var tx transaction.Transaction
	err = jsonx.Unmarshal(data, &tx)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction %s: %w", txHash, err)
	}

	return &tx, nil
}

// GetBatch retrieves multiple transactions by their hashes
func (ts *GenericTxStore) GetBatch(txHashes []string) ([]*transaction.Transaction, error) {
	if len(txHashes) == 0 {
		return []*transaction.Transaction{}, nil
	}

	ts.mu.RLock()
	defer ts.mu.RUnlock()

	transactions := make([]*transaction.Transaction, 0, len(txHashes))
	for _, txHash := range txHashes {
		// Direct database access without nested locking
		data, err := ts.dbProvider.Get(ts.getDbKey(txHash))
		if err != nil {
			logx.Warn("TX_STORE", fmt.Sprintf("Could not get transaction %s from database: %s", txHash, err.Error()))
			continue
		}

		// Deserialize transaction
		var tx transaction.Transaction
		err = jsonx.Unmarshal(data, &tx)
		if err != nil {
			logx.Warn("TX_STORE", fmt.Sprintf("Failed to unmarshal transaction %s: %s", txHash, err.Error()))
			continue
		}

		transactions = append(transactions, &tx)
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
