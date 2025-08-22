package poh

import (
	"crypto/sha256"
	"fmt"
)

func VerifyEntries(prev [32]byte, entries []Entry) error {
	fmt.Printf("VerifyEntries: prev hash: %x\n", prev)
	fmt.Printf("VerifyEntries: verifying %d entries\n", len(entries))
	cur := prev

	for i, e := range entries {
		// Do NumHashes-1 regular hashes first
		for n := uint64(0); n < e.NumHashes-1; n++ {
			cur = sha256.Sum256(cur[:])
		}

		// Then do the final hash
		if len(e.Transactions) == 0 {
			// Tick entry: one more regular hash
			cur = sha256.Sum256(cur[:])
		} else {
			// Transaction entry: hash with mixin
			mixin := HashTransactions(e.Transactions)
			cur = sha256.Sum256(append(cur[:], mixin[:]...))
		}

		if cur != e.Hash {
			return fmt.Errorf("PoH mismatch at entry %d: expected %x, got %x", i, e.Hash, cur)
		}
	}
	return nil
}
