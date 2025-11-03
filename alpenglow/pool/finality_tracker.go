package pool

type BlockId struct {
	Slot      uint64
	BlockHash [32]byte
}

type FinalizationStatus int

const (
	NOTARIZED FinalizationStatus = iota
	FINAL_PENDING_NOTAR
	FINALIZED
	IMPLICITLY_FINALIZED
	IMPLICITLY_SKIPPED
)

type StatusEntry struct {
	Status    FinalizationStatus
	BlockHash [32]byte
}

type FinalizationEvent struct {
	Finalized           *BlockId
	ImplicitlyFinalized []BlockId
	ImplicitlySkipped   []uint64
}

type FinalityTracker struct {
	status               map[uint64]StatusEntry
	parents              map[BlockId]BlockId
	highestFinalizedSlot uint64
}

func NewFinalityTracker() *FinalityTracker {
	return &FinalityTracker{
		status:               make(map[uint64]StatusEntry),
		parents:              make(map[BlockId]BlockId),
		highestFinalizedSlot: 0,
	}
}

func (ft *FinalityTracker) AddParent(block BlockId, parent BlockId) FinalizationEvent {
	if block.Slot <= parent.Slot {
		panic("block.Slot <= parent.Slot")
	}

	if currentParent, exists := ft.parents[block]; exists {
		if parent != currentParent {
			panic("consensus safety violation")
		}
		return FinalizationEvent{}
	} else {
		ft.parents[block] = parent
	}

	status, exists := ft.status[block.Slot]

	if !exists {
		return FinalizationEvent{}
	}

	if (status.Status == FINALIZED || status.Status == IMPLICITLY_FINALIZED) && status.BlockHash == block.BlockHash {
		event := FinalizationEvent{}
		ft.handleImplicitlyFinalized(block.Slot, parent, &event)
		return event
	}

	return FinalizationEvent{}
}

func (ft *FinalityTracker) GetHighestFinalizedSlot() uint64 {
	return ft.highestFinalizedSlot
}

func (ft *FinalityTracker) MarkNotarized(slot uint64, blockHash [32]byte) FinalizationEvent {
	old, exists := ft.status[slot]
	ft.status[slot] = StatusEntry{Status: NOTARIZED, BlockHash: blockHash}

	if !exists {
		return FinalizationEvent{}
	}

	switch old.Status {
	case NOTARIZED, FINALIZED, IMPLICITLY_FINALIZED:
		if old.BlockHash != blockHash {
			panic("consensus safety violation")
		}
		return FinalizationEvent{}

	case IMPLICITLY_SKIPPED:
		return FinalizationEvent{}

	case FINAL_PENDING_NOTAR:
		event := FinalizationEvent{}
		ft.status[slot] = StatusEntry{Status: FINALIZED, BlockHash: blockHash}
		ft.handleFinalizedBlock(BlockId{Slot: slot, BlockHash: blockHash}, &event)
		return event
	}

	return FinalizationEvent{}
}

func (ft *FinalityTracker) MarkFastFinalized(slot uint64, blockHash [32]byte) FinalizationEvent {
	old, exists := ft.status[slot]
	ft.status[slot] = StatusEntry{Status: FINALIZED, BlockHash: blockHash}

	if exists {
		switch old.Status {
		case FINALIZED, IMPLICITLY_FINALIZED:
			if old.BlockHash != blockHash {
				panic("consensus safety violation")
			}

		case NOTARIZED:
			if old.BlockHash != blockHash {
				panic("consensus safety violation")
			}

		case FINAL_PENDING_NOTAR:
			// do nothing

		case IMPLICITLY_SKIPPED:
			panic("consensus safety violation")
		}
	}

	event := FinalizationEvent{}
	ft.handleFinalizedBlock(BlockId{Slot: slot, BlockHash: blockHash}, &event)
	return event
}

func (ft *FinalityTracker) MarkFinalized(slot uint64) FinalizationEvent {
	old, exists := ft.status[slot]
	ft.status[slot] = StatusEntry{Status: FINAL_PENDING_NOTAR}
	if !exists {
		return FinalizationEvent{}
	}

	switch old.Status {
	case FINAL_PENDING_NOTAR, FINALIZED, IMPLICITLY_FINALIZED:
		return FinalizationEvent{}
	case NOTARIZED:
		event := FinalizationEvent{}
		ft.status[slot] = StatusEntry{Status: FINALIZED, BlockHash: old.BlockHash}
		ft.handleFinalizedBlock(BlockId{Slot: slot, BlockHash: old.BlockHash}, &event)
		return event
	case IMPLICITLY_SKIPPED:
		panic("consensus safety violation")
	}

	return FinalizationEvent{}
}

func (ft *FinalityTracker) handleFinalizedBlock(finalized BlockId, event *FinalizationEvent) {
	slot := finalized.Slot
	event.Finalized = &finalized
	if slot > ft.highestFinalizedSlot {
		ft.highestFinalizedSlot = slot
	}

	parent, exists := ft.parents[finalized]
	if !exists {
		ft.handleImplicitlyFinalized(slot, parent, event)
	}

}

func (ft *FinalityTracker) handleImplicitlyFinalized(sourceSlot uint64, implicitlyFinalized BlockId, event *FinalizationEvent) {

	// panic if sourceSlot <= implicitlyFinalized.Slot
	if sourceSlot <= implicitlyFinalized.Slot {
		panic("sourceSlot <= implicitlyFinalized.Slot")
	}

	// Append all implicitly skipped slots
	for slot := implicitlyFinalized.Slot + 1; slot < sourceSlot; slot++ {
		oldStatus := ft.status[slot]
		ft.status[slot] = StatusEntry{Status: IMPLICITLY_SKIPPED}

		switch oldStatus.Status {
		case IMPLICITLY_SKIPPED:
			return

		case NOTARIZED:
		// do nothing

		case FINAL_PENDING_NOTAR, FINALIZED, IMPLICITLY_FINALIZED:
			panic("consensus safety violation")

		}
		event.ImplicitlySkipped = append(event.ImplicitlySkipped, slot)
	}

	// Mark implicitly finalized slot
	oldStatus := ft.status[implicitlyFinalized.Slot]
	ft.status[implicitlyFinalized.Slot] = StatusEntry{Status: IMPLICITLY_FINALIZED, BlockHash: implicitlyFinalized.BlockHash}

	switch oldStatus.Status {
	case FINALIZED, IMPLICITLY_FINALIZED:
		if oldStatus.BlockHash != implicitlyFinalized.BlockHash {
			panic("consensus safety violation")
		}
		ft.status[implicitlyFinalized.Slot] = oldStatus
		return

	case NOTARIZED, FINAL_PENDING_NOTAR:
		// do nothing

	case IMPLICITLY_SKIPPED:
		panic("consensus safety violation")

	}
	event.ImplicitlyFinalized = append(event.ImplicitlyFinalized, implicitlyFinalized)

	// Recurse if needed
	if parent, exists := ft.parents[implicitlyFinalized]; exists {
		ft.handleImplicitlyFinalized(implicitlyFinalized.Slot, parent, event)
	}
}

func (ft *FinalityTracker) Prune() {
	for slot := range ft.status {
		if slot < ft.highestFinalizedSlot {
			delete(ft.status, slot)
		}
	}
	for block, parent := range ft.parents {
		if block.Slot < ft.highestFinalizedSlot && parent.Slot < ft.highestFinalizedSlot {
			delete(ft.parents, block)
		}
	}
}
