package mem_blockstore

import (
	"github.com/mezonai/mmn/block"
)

type SlotBlockData struct {
	primary     *block.BroadcastedBlock            // the block that first arrives for the slot
	repaired    map[string]*block.BroadcastedBlock // the blocks that have been repaired
	equivocated bool                               // whether there is equivocation in repaired blocks
}

func NewSlotBlockData() *SlotBlockData {
	return &SlotBlockData{
		primary:     nil,
		repaired:    make(map[string]*block.BroadcastedBlock),
		equivocated: false,
	}
}

func (s *SlotBlockData) AddBlock(block *block.BroadcastedBlock) {
	if s.primary != nil && s.primary.Hash != block.Hash {
		s.repaired[string(block.HashString())] = block
		s.equivocated = true
		return
	}
	s.primary = block
}

func (s *SlotBlockData) AddRepairedBlock(block *block.BroadcastedBlock) {
	s.repaired[block.HashString()] = block
	if s.primary != nil {
		s.equivocated = true
	}
}

func (s *SlotBlockData) GetPrimaryBlocks() *block.BroadcastedBlock {
	return s.primary
}

func (s *SlotBlockData) GetRepairedBlocks() map[string]*block.BroadcastedBlock {
	return s.repaired
}

func (s *SlotBlockData) GetBlock(blockHash [32]byte) *block.BroadcastedBlock {
	if s.primary != nil && s.primary.Hash == blockHash {
		return s.primary
	}
	for _, repairedBlock := range s.repaired {
		if repairedBlock.Hash == blockHash {
			return repairedBlock
		}
	}
	return nil
}
