package poh

import (
	"crypto/sha256"
	"fmt"

	"github.com/mezonai/mmn/logx"
)

func VerifyEntries(prev [32]byte, entries []Entry, slot uint64) error {
	logx.Info("POH", fmt.Sprintf("VerifyEntries: prev hash: %x", prev))
	logx.Info("POH", fmt.Sprintf("VerifyEntries: verifying %d entries", len(entries)))
	cur := prev

	for i, e := range entries {
		for n := uint64(0); n < e.NumHashes-1; n++ {
			cur = sha256.Sum256(cur[:])
		}

		if len(e.Transactions) == 0 {
			cur = sha256.Sum256(cur[:])
		} else {
			mixin := HashTransactions(e.Transactions)
			hash := sha256.Sum256(append(cur[:], mixin[:]...))
			copy(cur[:], hash[:])
		}

		if cur != e.Hash {
			return fmt.Errorf("PoH mismatch at entry=%d, slot=%d", i, slot)
		}
	}
	return nil
}
