package mempool

import (
	"time"

	"github.com/allegro/bigcache"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/store"
)

const (
	DedupSlotGap = 200
)

type DedupService struct {
	txDedupHashesCache *bigcache.BigCache
	bs                 store.BlockStore
	ts                 store.TxStore
}

func NewDedupService(bs store.BlockStore, ts store.TxStore) (*DedupService, error) {
	cacheConfig := bigcache.Config{
		Shards:           128,
		LifeWindow:       5 * time.Minute,
		CleanWindow:      10 * time.Minute,
		MaxEntrySize:     10,
		Verbose:          false,
		HardMaxCacheSize: 0,
	}

	cache, err := bigcache.NewBigCache(cacheConfig)
	if err != nil {
		return nil, err
	}

	ds := &DedupService{
		txDedupHashesCache: cache,
		bs:                 bs,
		ts:                 ts,
	}
	return ds, nil
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

	var txDedupHashes []string
	for _, block := range mapSlotBlock {
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
	}

	ds.Add(txDedupHashes)
}

func (ds *DedupService) IsDuplicate(txDedupHash string) (bool, error) {
	_, err := ds.txDedupHashesCache.Get(txDedupHash)
	switch err {
	case nil:
		return true, nil
	case bigcache.ErrEntryNotFound:
		return false, nil
	default:
		logx.Error("DEDUP SERVICE:IS DUPLICATE", "Error: ", err)
		return false, err
	}
}

func (ds *DedupService) Add(txDedupHashes []string) {
	for _, txDedupHash := range txDedupHashes {
		err := ds.txDedupHashesCache.Set(txDedupHash, []byte{1})
		if err != nil {
			logx.Error("DEDUP SERVICE:ADD", "Error: ", err)
		}
	}
}
