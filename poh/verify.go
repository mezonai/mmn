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
		for n := uint64(0); n < e.NumHashes-1; n++ {
			cur = sha256.Sum256(cur[:])
		}

		if len(e.TxHashes) == 0 {
			cur = sha256.Sum256(cur[:])
		} else {
			mixin := HashTransactions(e.TxHashes)
			hash := sha256.Sum256(append(cur[:], mixin[:]...))
			copy(cur[:], hash[:])
		}

		if cur != e.Hash {
			return fmt.Errorf("PoH mismatch at entry %d", i)
		}
	}
	return nil
}
