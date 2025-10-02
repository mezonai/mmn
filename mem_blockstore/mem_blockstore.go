package mem_blockstore

import (
	"github.com/mezonai/mmn/alpenglow/votor"
	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/poh"
)

type MemBlockStore struct {
	blockData    map[uint64]*SlotBlockData // slot -> SlotBlockData
	votorChannel chan votor.VotorEvent
}

func NewMemBlockStore(votorChannel chan votor.VotorEvent, genesisBlock *block.Block) *MemBlockStore {
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
		votorChannel: votorChannel,
	}
}

func (mbs *MemBlockStore) AddBlock(slot uint64, block *block.BroadcastedBlock) {
	slotBlockData, exists := mbs.blockData[slot]
	if !exists {
		slotBlockData = NewSlotBlockData()
		slotBlockData.AddBlock(block)
		mbs.blockData[slot] = slotBlockData
		return
	}

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
}

func (mbs *MemBlockStore) AddRepairedBlock(slot uint64, block *block.BroadcastedBlock) {
	slotBlockData, exists := mbs.blockData[slot]
	if !exists {
		slotBlockData = NewSlotBlockData()
		slotBlockData.AddRepairedBlock(block)
		mbs.blockData[slot] = slotBlockData
		return
	}
	slotBlockData.AddRepairedBlock(block)
	mbs.blockData[slot] = slotBlockData
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
