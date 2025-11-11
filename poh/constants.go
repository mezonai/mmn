package poh

const (
	// This prevents DoS attacks with extremely large NumHashes values
	MaxNumHashes = 100

	// This prevents memory exhaustion attacks by limiting entries per slot
	MaxEntriesPerSlot = 100

	// This prevents DoS attacks with extremely large transaction batches
	MaxTransactionsPerEntry = 6000

	// This prevents memory bombs with extremely large entries
	MaxEntrySize = 1024 * 1024

	// This prevents unbounded growth of the slot hash queue
	MaxSlotHashQueueSize = 10000
)
