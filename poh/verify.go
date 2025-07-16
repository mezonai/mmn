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

		if len(e.Transactions) == 0 {
			cur = sha256.Sum256(cur[:])
		} else {
			h := sha256.New()
			h.Write(cur[:])
			for _, raw := range e.Transactions {
				h.Write(raw)
			}
			copy(cur[:], h.Sum(nil))
		}

		if cur != e.Hash {
			return fmt.Errorf("PoH mismatch at entry %d", i)
		}
	}
	return nil
}
