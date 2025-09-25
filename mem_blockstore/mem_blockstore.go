package mem_blockstore

import (
	"github.com/mezonai/mmn/alpenglow/pool"
	"github.com/mezonai/mmn/alpenglow/votor"
	"github.com/mezonai/mmn/block"
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

func (mbs *MemBlockStore) AddBlock(slot uint64, block *block.BroadcastedBlock) {
	slotBlockData := mbs.getSlotBlockData(slot)
	slotBlockData.AddBlock(block)
	mbs.blockData[slot] = slotBlockData

	//TODO: optimize get parent block
	parentBlock := mbs.getParentBlock(block.Slot, block.PrevHash)

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

	mbs.pool.AddBlock(pool.BlockId{Slot: block.Slot, BlockHash: block.Hash}, pool.BlockId{Slot: 0, BlockHash: [32]byte{}})
}

func (mbs *MemBlockStore) getParentBlock(slot uint64, hash [32]byte) *block.BroadcastedBlock {
	// If parent is genesis block
	if slot == 1 {
		return mbs.blockData[0].GetPrimaryBlocks()
	}

	for s := slot - 1; s > 0; s-- {
		slotBlockData, exists := mbs.blockData[s]
		if !exists {
			continue
		}
		if slotBlockData.GetPrimaryBlocks() != nil && slotBlockData.GetPrimaryBlocks().LastEntryHash() == hash {
			return slotBlockData.GetPrimaryBlocks()
		}
		for _, repairedBlock := range slotBlockData.GetRepairedBlocks() {
			if repairedBlock.LastEntryHash() == hash {
				return repairedBlock
			}
		}
	}

	return nil
}
