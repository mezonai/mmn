package mempool

import (
	"context"
	"encoding/hex"
	"fmt"
	"mmn/interfaces"
	"mmn/types"
	"sync"
)

type Mempool struct {
	mu          sync.Mutex
	txsBuf      map[string][]byte
	max         int
	broadcaster interfaces.Broadcaster
}

func NewMempool(max int, broadcaster interfaces.Broadcaster) *Mempool {
	return &Mempool{txsBuf: make(map[string][]byte, max), max: max, broadcaster: broadcaster}
}

func (mp *Mempool) AddTx(tx *types.Transaction, broadcast bool) (string, bool) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	fmt.Println("Adding tx", tx)

	txBytes := tx.Bytes()
	txHash := hex.EncodeToString(txBytes)
	if _, du := mp.txsBuf[txHash]; du {
		fmt.Println("Dropping duplicate tx", txHash)
		return "", false // drop if duplicate
	}

	if len(mp.txsBuf) >= mp.max {
		fmt.Println("Dropping full mempool")
		return "", false // drop if full
	}

	mp.txsBuf[txHash] = txBytes
	if broadcast {
		mp.broadcaster.TxBroadcast(context.Background(), tx)
	}
	fmt.Println("Added tx", txHash)
	return txHash, true
}

// Pull batch of tx (for leader to batch and record)
// TODO: should keep order of txs
func (mp *Mempool) PullBatch(batchSize int) [][]byte {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	batch := make([][]byte, 0, batchSize)
	for id, raw := range mp.txsBuf {
		batch = append(batch, raw)
		delete(mp.txsBuf, id)
		if len(batch) >= batchSize {
			break
		}
	}
	return batch
}

// For demo: current number of tx
func (mp *Mempool) Size() int {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return len(mp.txsBuf)
}
