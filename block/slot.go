package block

type SlotInfo struct {
	Slot          uint64   `json:"slot"`
	PrevHash      [32]byte `json:"prev_hash"`
	LastEntryHash [32]byte `json:"last_entry_hash"` // hash of the last entry in the block, used for PoH
	LeaderID      string   `json:"leader_id"`
	Timestamp     uint64   `json:"timestamp"`
}
