package store

import (
	"encoding/hex"
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
	GetDBKey(txHash string) []byte
	GetLatestVersionContentKey(rootHash string) []byte
	GetLatestVersionContentHash(rootHash string) (string, error)
	GetBatchLatestVersionContentHash(rootHashes map[string]struct{}) (map[string]string, error)
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

		batch.Put(ts.GetDBKey(tx.Hash()), txData)
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
	data, err := ts.dbProvider.Get(ts.GetDBKey(txHash))
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
		keys[i] = ts.GetDBKey(txHash)
	}

	// Use true batch read - single CGO call!
	dataMap, err := ts.dbProvider.GetBatch(keys)
	if err != nil {
		return nil, fmt.Errorf("failed to batch get transactions: %w", err)
	}

	transactions := make([]*transaction.Transaction, 0, len(txHashes))

	for _, txHash := range txHashes {
		key := ts.GetDBKey(txHash)
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

// GetLatestVersionContentHash retrieves the latest version content hash for a given root hash
func (ts *GenericTxStore) GetLatestVersionContentHash(rootHash string) (string, error) {
	data, err := ts.dbProvider.Get(ts.GetLatestVersionContentKey(rootHash))
	if err != nil {
		return "", fmt.Errorf("could not get latest version content for root hash %s: %w", rootHash, err)
	}
	return hex.EncodeToString(data), nil
}

// GetBatchLatestVersionContentHash retrieves the latest version content hashes for multiple root hashes using true batch operation
func (ts *GenericTxStore) GetBatchLatestVersionContentHash(rootHashes map[string]struct{}) (map[string]string, error) {
	if len(rootHashes) == 0 {
		logx.Info("TX_STORE", "GetBatchLatestVersionContentHash: no root hashes provided")
		return map[string]string{}, nil
	}

	// Prepare keys for batch operation
	keys := make([][]byte, 0)
	for rootHash := range rootHashes {
		keys = append(keys, ts.GetLatestVersionContentKey(rootHash))
	}

	// Use true batch read - single CGO call!
	dataMap, err := ts.dbProvider.GetBatch(keys)
	if err != nil {
		return nil, fmt.Errorf("failed to batch get latest version content hashes: %w", err)
	}

	result := make(map[string]string)
	for rootHash := range rootHashes {
		key := ts.GetLatestVersionContentKey(rootHash)
		data, exists := dataMap[string(key)]
		if !exists {
			logx.Warn("TX_STORE", fmt.Sprintf("Latest version content for root hash %s not found in batch result", rootHash))
			continue
		}
		result[rootHash] = hex.EncodeToString(data)
	}
	return result, nil
}

// MustClose closes the transaction store and related resources
func (ts *GenericTxStore) MustClose() {
	err := ts.dbProvider.Close()
	if err != nil {
		logx.Error("TX_STORE", "Failed to close provider")
	}
}

func (ts *GenericTxStore) GetDBKey(txHash string) []byte {
	return []byte(PrefixTx + txHash)
}

func (ts *GenericTxStore) GetLatestVersionContentKey(rootHash string) []byte {
	return []byte(PrefixLatestVersionContent + rootHash)
}
