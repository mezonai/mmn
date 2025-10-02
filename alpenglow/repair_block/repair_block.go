package repair_block

import (
	"context"

	"github.com/mezonai/mmn/alpenglow/pool"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mem_blockstore"
	"github.com/mezonai/mmn/p2p"
)

type RepairBlockWorker struct {
	repairChanel  <-chan pool.BlockId
	memBlockStore *mem_blockstore.MemBlockStore
	ln            *p2p.Libp2pNetwork
}

func NewRepairBlockWorker(repairChanel <-chan pool.BlockId, memBlockStore *mem_blockstore.MemBlockStore, ln *p2p.Libp2pNetwork) *RepairBlockWorker {
	return &RepairBlockWorker{
		repairChanel:  repairChanel,
		memBlockStore: memBlockStore,
		ln:            ln,
	}
}

func (rpw *RepairBlockWorker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case blockId := <-rpw.repairChanel:
			blk := rpw.memBlockStore.GetBlock(blockId.Slot, blockId.BlockHash)
			if blk != nil {
				continue
			}

			blkResp, err := rpw.ln.RequestRepairBlock(ctx, blockId.Slot, blockId.BlockHash)
			if err != nil {
				logx.Error("REPAIR BLOCK", "Failed to repair block for slot", blockId.Slot, err)
				continue
			}

			rpw.memBlockStore.AddRepairedBlock(blockId.Slot, blkResp)
			logx.Info("REPAIR BLOCK", "Block repaired and saved for slot", blockId.Slot)
		}
	}
}
