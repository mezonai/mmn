package poh

const (
	// This prevents DoS attacks with extremely large NumHashes values
	MAX_NUM_HASHES = 10000

	// This prevents memory exhaustion attacks by limiting entries per slot
	MAX_ENTRIES_PER_SLOT = 1000

	// This prevents unbounded memory growth in the entries buffer
	MAX_ENTRIES_MEMORY = 100000

	// This prevents DoS attacks with extremely large transaction batches
	MAX_TRANSACTIONS_PER_ENTRY = 100

	// This prevents memory bombs with extremely large entries
	MAX_ENTRY_SIZE = 1024 * 1024

	// This prevents unbounded growth of the slot hash queue
	MAX_SLOT_HASH_QUEUE_SIZE = 10000
)

const (
	POH_DOMAIN_PREFIX = "POH:"
)
