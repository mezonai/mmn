package poh

import "mmn/types"

type Entry struct {
	NumHashes    uint64               `json:"num_hashes"`
	Hash         [32]byte             `json:"hash"`
	Transactions []*types.Transaction `json:"transactions"` // serialized txs
	Tick         bool                 `json:"tick"`
}

type PersistentEntry struct {
	NumHashes uint64   `json:"num_hashes"`
	Hash      [32]byte `json:"hash"`
	TxHashes  []string `json:"tx_hashes"`
	Tick      bool     `json:"tick"`
}

// Entry with no transactions (e.g. tick-only)
func NewTickEntry(numHashes uint64, hash [32]byte) Entry {
	return Entry{
		NumHashes:    numHashes,
		Hash:         hash,
		Transactions: nil,
		Tick:         true,
	}
}

// Entry with txs
func NewTxEntry(numHashes uint64, hash [32]byte, txs []*types.Transaction) Entry {
	return Entry{
		NumHashes:    numHashes,
		Hash:         hash,
		Transactions: txs,
		Tick:         false,
	}
}

// Empty entry check
func (e Entry) IsTickOnly() bool {
	return len(e.Transactions) == 0
}
