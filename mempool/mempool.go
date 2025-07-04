package mempool

import (
	"sync"
)

// Mempool provides a thread-safe queue for incoming transactions.
type Mempool struct {
	mu  sync.Mutex
	txs [][]byte
}

// NewMempool creates a new, empty mempool.
func NewMempool() *Mempool {
	return &Mempool{
		txs: make([][]byte, 0),
	}
}

// Add pushes a transaction into the mempool.
func (m *Mempool) Add(tx []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txs = append(m.txs, tx)
}

// Len returns the number of transactions in the mempool.
func (m *Mempool) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.txs)
}

// GetBatch returns up to max transactions without removing them.
func (m *Mempool) GetBatch(max int) [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.txs) == 0 {
		return nil
	}
	if len(m.txs) < max {
		max = len(m.txs)
	}
	batch := make([][]byte, max)
	copy(batch, m.txs[:max])
	return batch
}

// RemoveBatch removes the first n transactions from the mempool.
func (m *Mempool) RemoveBatch(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n >= len(m.txs) {
		m.txs = m.txs[:0]
	} else {
		m.txs = m.txs[n:]
	}
}
