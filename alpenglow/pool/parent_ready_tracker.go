package pool

import "github.com/mezonai/mmn/utils"

type ParentReadyState struct {
	Skip           bool
	NotarFallbacks [][32]byte
	IsReady        BlockId //TODO: IsReady NotReady (has someone waiting to hear when the slot ready) - Like Solana
}

type ParentReadyTracker struct {
	States map[uint64]*ParentReadyState
}

type SlotBlockId struct {
	Slot    uint64
	BlockId BlockId
}

func NewParentReadyTracker(genesisSlot uint64, genesisHash [32]byte) *ParentReadyTracker {
	states := make(map[uint64]*ParentReadyState)
	states[genesisSlot] = &ParentReadyState{
		NotarFallbacks: [][32]byte{genesisHash},
	}
	return &ParentReadyTracker{States: states}
}

func (prt *ParentReadyTracker) MarkNotarFallback(blockId BlockId) []SlotBlockId {

	if prt.States[blockId.Slot].isContainedNotarFallback(blockId.BlockHash) {
		return []SlotBlockId{}
	}
	state := prt.States[blockId.Slot]
	state.NotarFallbacks = append(state.NotarFallbacks, blockId.BlockHash)

	newlyCertified := []SlotBlockId{}

	for slot := blockId.Slot + 1; ; slot++ {
		state := prt.States[slot]
		if utils.IsSlotStartOfWindow(slot) {
			state.addToReady(blockId)
			newlyCertified = append(newlyCertified, SlotBlockId{Slot: slot, BlockId: blockId})
		}
		if !state.Skip {
			break
		}

	}
	return newlyCertified
}

func (prt *ParentReadyTracker) MarkSkipped(markedSlot uint64) []SlotBlockId {
	state := prt.States[markedSlot]
	if state.Skip {
		return nil
	}
	state.Skip = true

	// Find all potential parents
	potentialParents := []BlockId{}
	for slot := markedSlot; slot >= utils.FirstSlotInWindow(markedSlot); slot-- {
		state := prt.States[slot]

		if slot != markedSlot {
			for _, nf := range state.NotarFallbacks {
				potentialParents = append(potentialParents, BlockId{Slot: slot, BlockHash: nf})
			}
		}

		if !state.Skip {
			break
		}

		potentialParents = append(potentialParents, state.IsReady)
	}

	// Add parent for future slots
	newlyCertified := []SlotBlockId{}
	for slot := markedSlot + 1; ; slot++ {
		state := prt.States[slot]
		if utils.IsSlotStartOfWindow(slot) {
			for _, parent := range potentialParents {
				state.addToReady(parent)
			}
		}
		if !state.Skip {
			break
		}
	}
	return newlyCertified
}

func (prt *ParentReadyTracker) HandleFinalization(event FinalizationEvent) []SlotBlockId {

	parentsReady := []SlotBlockId{}
	if event.Finalized != nil {
		parentsReady = append(parentsReady, prt.MarkNotarFallback(*event.Finalized)...)
	}

	for _, blockId := range event.ImplicitlyFinalized {
		parentsReady = append(parentsReady, prt.MarkNotarFallback(blockId)...)
	}

	for _, slot := range event.ImplicitlySkipped {
		parentsReady = append(parentsReady, prt.MarkSkipped(slot)...)
	}

	var maxParent *SlotBlockId
	for i := range parentsReady {
		if maxParent == nil || parentsReady[i].Slot > maxParent.Slot {
			maxParent = &parentsReady[i]
		}
	}
	if maxParent != nil {
		return []SlotBlockId{*maxParent}
	}
	return nil
}

// ParentReadyState methods
func (prs *ParentReadyState) addToReady(blockId BlockId) {
	if prs.IsReady == (BlockId{}) {
		prs.IsReady = blockId
	} else {
		if prs.IsReady != blockId {
			panic("Inconsistent ready state")
		}
	}
}

func (prs *ParentReadyState) isContainedNotarFallback(blockHash [32]byte) bool {
	for _, nf := range prs.NotarFallbacks {
		if nf == blockHash {
			return true
		}
	}
	return false
}
