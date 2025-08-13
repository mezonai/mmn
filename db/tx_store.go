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
	db          *grocksdb.DB
	cfTxHash    *grocksdb.ColumnFamilyHandle
	cfTxAccount *grocksdb.ColumnFamilyHandle
	mu          sync.RWMutex
}

// NewTxRocksStore creates a new transaction store
func NewTxRocksStore(rocks *RocksDB) (*TxRocksStore, error) {
	return &TxRocksStore{
		db:          rocks.DB,
		cfTxHash:    rocks.MustGetColumnFamily(CfTxHash),
		cfTxAccount: rocks.MustGetColumnFamily(CfAccount),
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

		senderKey := fmt.Sprintf("%s:sender", tx.Sender)
		wb.PutCF(ts.cfTxAccount, []byte(senderKey), txHashBytes)

		recipientKey := fmt.Sprintf("%s:recipient", tx.Recipient)
		wb.PutCF(ts.cfTxAccount, []byte(recipientKey), txHashBytes)
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

	transactions := make([]*types.Transaction, 0, len(txHashes))
	notFound := make([]string, 0)

	// Retrieve transactions one by one using GetCF for each hash
	for _, txHash := range txHashes {
		data, err := ts.db.GetCF(ro, ts.cfTxHash, []byte(txHash))
		if err != nil {
			return nil, fmt.Errorf("failed to read transaction %s: %w", txHash, err)
		}

		if !data.Exists() {
			notFound = append(notFound, txHash)
			data.Free()
			continue
		}

		// Deserialize transaction
		var tx types.Transaction
		err = json.Unmarshal(data.Data(), &tx)
		data.Free()
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal transaction %s: %w", txHash, err)
		}

		transactions = append(transactions, &tx)
	}

	// If some transactions were not found, return an error with details
	if len(notFound) > 0 {
		return transactions, fmt.Errorf("some transactions not found: %v", notFound)
	}

	return transactions, nil
}
