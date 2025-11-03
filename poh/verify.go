package poh

import (
	"crypto/sha256"
	"fmt"

	"github.com/mezonai/mmn/logx"
)

func VerifyEntries(entries []Entry, slot uint64) error {
	logx.Info("POH", fmt.Sprintf("VerifyEntries: verifying %d entries in slot=%d", len(entries), slot))
	cur := entries[0].Hash

	for i := 1; i < len(entries); i++ {
		for n := uint64(0); n < entries[i].NumHashes-1; n++ {
			cur = sha256.Sum256(cur[:])
		}

		if len(entries[i].Transactions) == 0 {
			cur = sha256.Sum256(cur[:])
		} else {
			mixin := HashTransactions(entries[i].Transactions)
			hash := sha256.Sum256(append(cur[:], mixin[:]...))
			copy(cur[:], hash[:])
		}

		if cur != entries[i].Hash {
			return fmt.Errorf("PoH mismatch: entry=%d slot=%d expected=%x got=%x", i, slot, entries[i].Hash, cur)
		}
	}

	return nil
}
