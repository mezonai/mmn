package store

import (
	"fmt"

	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"

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
	logx.Info("TX_STORE", fmt.Sprintf("StoreBatch: storing %d transactions", len(txs)))

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

	logx.Info("TX_STORE", fmt.Sprintf("StoreBatch: stored %d transactions", len(txs)))
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

// GetBatch retrieves multiple transactions by their hashes using true batch operation
func (ts *GenericTxStore) GetBatch(txHashes []string) ([]*transaction.Transaction, error) {
	if len(txHashes) == 0 {
		logx.Info("TX_STORE", "GetBatch: no transactions to retrieve")
		return []*transaction.Transaction{}, nil
	}
	logx.Info("TX_STORE", fmt.Sprintf("GetBatch: retrieving %d transactions", len(txHashes)))

	// Prepare keys for batch operation
	keys := make([][]byte, len(txHashes))
	for i, txHash := range txHashes {
		keys[i] = ts.getDbKey(txHash)
	}

	// Use true batch read - single CGO call!
	dataMap, err := ts.dbProvider.GetBatch(keys)
	if err != nil {
		return nil, fmt.Errorf("failed to batch get transactions: %w", err)
	}

	transactions := make([]*transaction.Transaction, 0, len(txHashes))

	for _, txHash := range txHashes {
		key := ts.getDbKey(txHash)
		data, exists := dataMap[string(key)]

		if !exists {
			logx.Warn("TX_STORE", fmt.Sprintf("Transaction %s not found in batch result", txHash))
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

	logx.Info("TX_STORE", fmt.Sprintf("GetBatch: retrieved %d/%d transactions", len(transactions), len(txHashes)))
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
