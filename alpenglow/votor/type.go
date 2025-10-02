package votor

import "github.com/mezonai/mmn/consensus"

type ParentReadyKey struct {
	Slot       uint64
	ParentSlot uint64
	ParentHash [32]byte
}

type BlockInfo struct {
	Slot       uint64
	Hash       [32]byte
	ParentSlot uint64
	ParentHash [32]byte
}

type VotorEventType int

const (
	BLOCK_RECEIVED VotorEventType = iota
	PARENT_READY
	SAFE_TO_NOTAR
	SAFE_TO_SKIP
	CERT_CREATED
	CERT_SAVED
	REPAIR_NEEDED
	TIMEOUT
)

type VotorEvent struct {
	Type      VotorEventType
	Slot      uint64
	BlockHash [32]byte
	Block     BlockInfo
	Cert      *consensus.Cert
}
