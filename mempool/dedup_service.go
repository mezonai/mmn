package mempool

import (
	"hash/fnv"
	"sync"

	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/store"
)

const (
	DedupSlotGap = 200
	NumShards     = 128
)

type slotEntry struct {
	mu            sync.Mutex
	txDedupHashes []string
}

type dedupShard struct {
	mu             sync.RWMutex
	txDedupHashSet map[string]struct{}
}

type DedupService struct {
	txShards  [NumShards]*dedupShard
	slotEntry sync.Map // map[uint64]*slotEntry
	bs        store.BlockStore
	ts        store.TxStore
}

func NewDedupService(bs store.BlockStore, ts store.TxStore) *DedupService {
	ds := &DedupService{
		bs: bs,
		ts: ts,
	}

	for i := 0; i < NumShards; i++ {
		ds.txShards[i] = &dedupShard{
			txDedupHashSet: make(map[string]struct{}),
		}
	}

	return ds
}

func (ds *DedupService) LoadTxHashes(latestSlot uint64) {
	if latestSlot < 1 {
		return
	}

	startSlot := uint64(1)
	if latestSlot > DedupSlotGap {
		startSlot = latestSlot - DedupSlotGap + 1
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
	shardIndex := getShardIndex(txDedupHash)
	shard := ds.txShards[shardIndex]
	shard.mu.RLock()
	_, exists := shard.txDedupHashSet[txDedupHash]
	shard.mu.RUnlock()
	return exists
}

func (ds *DedupService) Add(slot uint64, txDedupHashes []string) {
	v, _ := ds.slotEntry.LoadOrStore(slot, &slotEntry{})

	se := v.(*slotEntry)
	se.mu.Lock()
	se.txDedupHashes = append(se.txDedupHashes, txDedupHashes...)
	se.mu.Unlock()

	for _, txDedupHash := range txDedupHashes {
		shardIndex := getShardIndex(txDedupHash)
		shard := ds.txShards[shardIndex]
		shard.mu.Lock()
		shard.txDedupHashSet[txDedupHash] = struct{}{}
		shard.mu.Unlock()
	}
}

func (ds *DedupService) CleanUpOldSlotTxHashes(slot uint64) {
	if slot <= DedupSlotGap {
		return
	}
	oldSlot := slot - DedupSlotGap

	v, ok := ds.slotEntry.LoadAndDelete(oldSlot)
	if !ok {
		return
	}

	se := v.(*slotEntry)
	se.mu.Lock()
	txDedupHashes := se.txDedupHashes
	se.mu.Unlock()

	for _, txDedupHash := range txDedupHashes {
		shardIndex := getShardIndex(txDedupHash)
		shard := ds.txShards[shardIndex]
		shard.mu.Lock()
		delete(shard.txDedupHashSet, txDedupHash)
		shard.mu.Unlock()
	}
}

func getShardIndex(key string) uint {
	h := fnv.New32a()
	h.Write([]byte(key))
	return uint(h.Sum32() % NumShards)
}
