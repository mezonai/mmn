package mempool

import (
	"sync"

	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/store"
)

const (
	DEDUP_SLOT_GAP = 200
)

type DedupService struct {
	mu                 sync.RWMutex
	txDedupHashSet     map[string]struct{}
	slotTxDedupHashSet map[uint64]map[string]struct{}

	bs store.BlockStore
	ts store.TxStore
}

func NewDedupService(bs store.BlockStore, ts store.TxStore) *DedupService {
	return &DedupService{
		txDedupHashSet:     make(map[string]struct{}),
		slotTxDedupHashSet: make(map[uint64]map[string]struct{}),
		bs:                 bs,
		ts:                 ts,
	}
}

func (ds *DedupService) LoadTxHashes(latestSlot uint64) {
	if latestSlot < 1 {
		return
	}

	startSlot := uint64(1)
	if latestSlot > DEDUP_SLOT_GAP {
		startSlot = latestSlot - DEDUP_SLOT_GAP + 1
	}

	loadSlots := make([]uint64, 0, latestSlot-startSlot+1)
	for i := startSlot; i <= latestSlot; i++ {
		loadSlots = append(loadSlots, i)
	}

	mapSlotBlock, err := ds.bs.GetBatch(loadSlots)
	if err != nil {
		logx.Error("DEDUP SERVICE:LOAD TX HASHES", "Error: ", err)
		return
	}

	for slot, block := range mapSlotBlock {
		var txDedupHashes []string
		var txHashes []string
		for _, entry := range block.Entries {
			txHashes = append(txHashes, entry.TxHashes...)
		}

		txs, err := ds.ts.GetBatch(txHashes)
		if err != nil {
			logx.Error("DEDUP SERVICE:LOAD TX HASHES", "Error: ", err)
			continue
		}
		for _, tx := range txs {
			txDedupHashes = append(txDedupHashes, tx.DedupHash())
		}

		ds.Add(slot, txDedupHashes)
	}
}

func (ds *DedupService) IsDuplicate(txDedupHash string) bool {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	_, exists := ds.txDedupHashSet[txDedupHash]
	return exists
}

func (ds *DedupService) Add(slot uint64, txDedupHashes []string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Add new tx hashes
	for _, txDedupHash := range txDedupHashes {
		ds.txDedupHashSet[txDedupHash] = struct{}{}
		if _, exists := ds.slotTxDedupHashSet[slot]; !exists {
			ds.slotTxDedupHashSet[slot] = make(map[string]struct{})
		}
		ds.slotTxDedupHashSet[slot][txDedupHash] = struct{}{}
	}
}

func (ds *DedupService) CleanUpOldSlotTxHashes(slot uint64) {
	if slot <= DEDUP_SLOT_GAP {
		return
	}

	// Clean up old slot tx hashes
	oldSlot := slot - DEDUP_SLOT_GAP
	if oldTxDedupHashes, exists := ds.slotTxDedupHashSet[oldSlot]; exists {
		for oldTxDedupHash := range oldTxDedupHashes {
			delete(ds.txDedupHashSet, oldTxDedupHash)
		}
		delete(ds.slotTxDedupHashSet, oldSlot)
	}
}
