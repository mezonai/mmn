package types

import (
	"github.com/holiman/uint256"
)

type TxRecord struct {
	Slot      uint64
	Amount    *uint256.Int
	Sender    string
	Recipient string
	Timestamp uint64
	TextData  string
	Type      int32
	Nonce     uint64
}

type Account struct {
	Address string      `json:"address"`
	Balance *uint256.Int `json:"balance"`
	Nonce   uint64      `json:"nonce"`
	History []string    `json:"history"` // tx hashes
}

type SnapshotAccount struct {
	Balance *uint256.Int `json:"balance"`
	Nonce   uint64      `json:"nonce"`
}
