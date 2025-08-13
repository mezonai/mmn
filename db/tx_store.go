package db

import (
	"encoding/json"
	"fmt"
	"sync"

	"mmn/types"

	"github.com/linxGnu/grocksdb"
)

type TxStore interface {
	Store(tx *types.Transaction) error
	StoreBatch(txs []*types.Transaction) error
	GetByHash(txHash string) (*types.Transaction, error)
	GetBatch(txHashes []string) ([]*types.Transaction, error)
}

// TxRocksStore provides transaction storage operations
type TxRocksStore struct {
	db       *grocksdb.DB
	cfTxHash *grocksdb.ColumnFamilyHandle
	mu       sync.RWMutex
}

// NewTxRocksStore creates a new transaction store
func NewTxRocksStore(rocks *RocksDB) (*TxRocksStore, error) {
	return &TxRocksStore{
		db:       rocks.DB,
		cfTxHash: rocks.MustGetColumnFamily(CfTxHash),
	}, nil
}

// Store stores a transaction in the database
func (ts *TxRocksStore) Store(tx *types.Transaction) error {
	return ts.StoreBatch([]*types.Transaction{tx})
}

// StoreBatch stores a batch of transactions in the database
func (ts *TxRocksStore) StoreBatch(txs []*types.Transaction) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	wb := grocksdb.NewWriteBatch()
	defer wb.Destroy()

	for _, tx := range txs {
		txHashBytes := []byte(tx.Hash())
		txData, err := json.Marshal(tx)
		if err != nil {
			return fmt.Errorf("failed to marshal transaction: %w", err)
		}

		wb.PutCF(ts.cfTxHash, txHashBytes, txData)
	}

	wo := grocksdb.NewDefaultWriteOptions()
	defer wo.Destroy()

	err := ts.db.Write(wo, wb)
	if err != nil {
		return fmt.Errorf("failed to write transaction to database: %w", err)
	}

	return nil
}

// GetByHash retrieves a transaction by its hash
func (ts *TxRocksStore) GetByHash(txHash string) (*types.Transaction, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	ro := grocksdb.NewDefaultReadOptions()
	defer ro.Destroy()

	data, err := ts.db.GetCF(ro, ts.cfTxHash, []byte(txHash))
	if err != nil {
		return nil, fmt.Errorf("failed to read transaction data: %w", err)
	}
	defer data.Free()

	if !data.Exists() {
		return nil, fmt.Errorf("transaction not found: %s", txHash)
	}

	// Deserialize transaction
	var tx types.Transaction
	err = json.Unmarshal(data.Data(), &tx)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction: %w", err)
	}

	return &tx, nil
}

// GetBatch retrieves multiple transactions by their hashes
func (ts *TxRocksStore) GetBatch(txHashes []string) ([]*types.Transaction, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if len(txHashes) == 0 {
		return []*types.Transaction{}, nil
	}

	ro := grocksdb.NewDefaultReadOptions()
	defer ro.Destroy()

	txHashesBytes := make([][]byte, 0, len(txHashes))
	for _, txHash := range txHashes {
		txHashesBytes = append(txHashesBytes, []byte(txHash))
	}

	transactions := make([]*types.Transaction, 0, len(txHashes))
	txs, err := ts.db.MultiGetCF(ro, ts.cfTxHash, txHashesBytes...)
	if err != nil {
		return nil, fmt.Errorf("failed to read transactions: %w", err)
	}
	for i, tx := range txs {
		if !tx.Exists() {
			tx.Free()
			continue
		}

		var t types.Transaction
		err = json.Unmarshal(tx.Data(), &t)
		tx.Free()
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal transaction %s: %w", txHashes[i], err)
		}
		transactions = append(transactions, &t)
	}

	return transactions, nil
}
