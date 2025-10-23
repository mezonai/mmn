package mempool

import (
	"sync"
)

const (
	DEDUP_SLOT_GAP = 200
)

type DedupService struct {
	mu                   sync.RWMutex
	setTxDedupHashes     map[string]struct{}
	mapSlotTxDedupHashes map[uint64]map[string]struct{}
}

func NewDedupService() *DedupService {
	return &DedupService{
		setTxDedupHashes:     make(map[string]struct{}),
		mapSlotTxDedupHashes: make(map[uint64]map[string]struct{}),
	}
}

func (ds *DedupService) IsDuplicate(txDedupHash string) bool {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	_, exists := ds.setTxDedupHashes[txDedupHash]
	return exists
}

func (ds *DedupService) Add(slot uint64, TxDedupHashes []string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Clean up old slot tx hashes
	if slot > DEDUP_SLOT_GAP {
		oldSlot := slot - DEDUP_SLOT_GAP
		if oldTxDedupHashes, exists := ds.mapSlotTxDedupHashes[oldSlot]; exists {
			for oldTxDedupHash := range oldTxDedupHashes {
				delete(ds.setTxDedupHashes, oldTxDedupHash)
			}
			delete(ds.mapSlotTxDedupHashes, oldSlot)
		}
	}

	// Add new tx hashes
	for _, txDedupHash := range TxDedupHashes {
		ds.setTxDedupHashes[txDedupHash] = struct{}{}
		if _, exists := ds.mapSlotTxDedupHashes[slot]; !exists {
			ds.mapSlotTxDedupHashes[slot] = make(map[string]struct{})
		}
		ds.mapSlotTxDedupHashes[slot][txDedupHash] = struct{}{}
	}
}
