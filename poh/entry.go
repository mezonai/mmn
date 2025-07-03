package poh

type Entry struct {
	NumHashes    uint64   `json:"num_hashes"`
	Hash         [32]byte `json:"hash"`
	Transactions [][]byte `json:"transactions"` // serialized txs
}

// Entry with no transactions (e.g. tick-only)
func NewTickEntry(numHashes uint64, hash [32]byte) Entry {
	return Entry{
		NumHashes:    numHashes,
		Hash:         hash,
		Transactions: nil,
	}
}

// Entry with txs
func NewTxEntry(numHashes uint64, hash [32]byte, txs [][]byte) Entry {
	return Entry{
		NumHashes:    numHashes,
		Hash:         hash,
		Transactions: txs,
	}
}

// Empty entry check
func (e Entry) IsTickOnly() bool {
	return len(e.Transactions) == 0
}
