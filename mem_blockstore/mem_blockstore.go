package mem_blockstore

import (
	"fmt"

	"github.com/mezonai/mmn/alpenglow/pool"
	"github.com/mezonai/mmn/alpenglow/votor"
	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/poh"
)

// TODO: need logic to cleanup old slots
type MemBlockStore struct {
	blockData    map[uint64]*SlotBlockData // slot -> SlotBlockData
	pool         *pool.Pool
	votorChannel chan votor.VotorEvent
}

func NewMemBlockStore(votorChannel chan votor.VotorEvent, pool *pool.Pool, genesisBlock *block.Block) *MemBlockStore {
	// Create a genesisBlockData from genesisBlock
	genesisBlockData := &block.BroadcastedBlock{
		BlockCore: genesisBlock.BlockCore,
		Entries:   []poh.Entry{poh.NewTickEntry(1, [32]byte{})},
	}

	slotBlockData := NewSlotBlockData()
	slotBlockData.AddBlock(genesisBlockData)
	blockData := make(map[uint64]*SlotBlockData)
	blockData[0] = slotBlockData
	return &MemBlockStore{
		blockData:    blockData,
		pool:         pool,
		votorChannel: votorChannel,
	}
}

func (mbs *MemBlockStore) getSlotBlockData(slot uint64) *SlotBlockData {
	slotBlockData, exists := mbs.blockData[slot]
	if !exists {
		slotBlockData = NewSlotBlockData()
		mbs.blockData[slot] = slotBlockData
	}
	return slotBlockData
}

func (mbs *MemBlockStore) GetBlock(slot uint64, blockHash [32]byte) *block.BroadcastedBlock {
	if slotBlockData, exists := mbs.blockData[slot]; exists {
		if blk := slotBlockData.GetBlock(blockHash); blk != nil {
			return blk
		}
	}
	return nil
}

func (mbs *MemBlockStore) AddBlock(block *block.BroadcastedBlock) {
	slot := block.Slot
	slotBlockData := mbs.getSlotBlockData(slot)
	slotBlockData.AddBlock(block)
	mbs.blockData[slot] = slotBlockData

	logx.Info("MEM_BLOCKSTORE", fmt.Sprintf("Added block %s in slot %d", block.HashString(), slot))

	parentBlock := mbs.GetParentBlock(block.Slot, block.PrevHash)
	logx.Info("MEM_BLOCKSTORE", fmt.Sprintf("Parent block of block %s in slot %d is %s in slot %d", block.HashString(), slot, parentBlock.HashString(), parentBlock.Slot))

	blockInfo := votor.BlockInfo{
		Slot:       block.Slot,
		Hash:       block.Hash,
		ParentSlot: parentBlock.Slot,
		ParentHash: parentBlock.Hash,
	}

	mbs.votorChannel <- votor.VotorEvent{
		Type:      votor.BLOCK_RECEIVED,
		Slot:      slot,
		BlockHash: block.Hash,
		Block:     blockInfo,
	}

	mbs.pool.AddBlock(pool.BlockId{Slot: block.Slot, BlockHash: block.Hash}, pool.BlockId{Slot: parentBlock.Slot, BlockHash: parentBlock.Hash})
}

func (mbs *MemBlockStore) AddRepairedBlock(slot uint64, block *block.BroadcastedBlock) {
	slotBlockData := mbs.getSlotBlockData(slot)
	slotBlockData.AddRepairedBlock(block)
	mbs.blockData[slot] = slotBlockData

	parentBlock := mbs.GetParentBlock(block.Slot, block.PrevHash)

	mbs.pool.AddBlock(pool.BlockId{Slot: block.Slot, BlockHash: block.Hash}, pool.BlockId{Slot: parentBlock.Slot, BlockHash: parentBlock.Hash})
}

func (mbs *MemBlockStore) Prune(finalizedSlot uint64) {
	logx.Info("MEM_BLOCKSTORE", fmt.Sprintf("Pruned store before slot %d", finalizedSlot))
	for slot := range mbs.blockData {
		if slot < finalizedSlot {
			delete(mbs.blockData, slot)
		}
	}
}

func (mbs *MemBlockStore) GetParentBlock(slot uint64, prevHash [32]byte) *block.BroadcastedBlock {
	// If parent is genesis block
	if slot == 1 {
		return mbs.blockData[0].GetPrimaryBlocks()
	}

	for s := slot - 1; s > 0; s-- {
		if slotBlockData, exists := mbs.blockData[s]; exists {
			if blk := slotBlockData.GetBlock(prevHash); blk != nil {
				return blk
			}
		}
	}

	return nil
}
