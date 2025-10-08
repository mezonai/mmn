package utils

const (
	SLOTS_PER_WINDOW = 10 //TODO: not hardcode, read from config
)

func IsSlotStartOfWindow(slot uint64) bool {
	return (slot-1)%SLOTS_PER_WINDOW == 0
}

func FirstSlotInWindow(slot uint64) uint64 {
	window := slot / SLOTS_PER_WINDOW
	return window*SLOTS_PER_WINDOW + 1
}

func LastSlotInWindow(slot uint64) uint64 {
	window := slot / SLOTS_PER_WINDOW
	return (window + 1) * SLOTS_PER_WINDOW
}

func SlotsInWindow(slot uint64) []uint64 {
	first := FirstSlotInWindow(slot)
	slots := make([]uint64, SLOTS_PER_WINDOW)
	for i := uint64(0); i < SLOTS_PER_WINDOW; i++ {
		slots[i] = first + i
	}
	return slots
}
