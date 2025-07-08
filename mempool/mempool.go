package mempool

import (
	"fmt"
	"sync"
)

type Mempool struct {
	mu  sync.Mutex
	buf [][]byte
	max int
}

func NewMempool(max int) *Mempool {
	return &Mempool{buf: make([][]byte, 0, max), max: max}
}

func (mp *Mempool) AddTx(tx []byte) bool {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	fmt.Println("Adding tx", string(tx))
	if len(mp.buf) >= mp.max {
		return false // drop if full
	}
	mp.buf = append(mp.buf, tx)
	return true
}

// Pull batch of tx (for leader to batch and record)
func (mp *Mempool) PullBatch(batchSize int) [][]byte {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	n := batchSize
	if n > len(mp.buf) {
		n = len(mp.buf)
	}
	batch := mp.buf[:n]
	mp.buf = mp.buf[n:]
	return batch
}

// For demo: current number of tx
func (mp *Mempool) Size() int {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return len(mp.buf)
}
