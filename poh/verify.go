package poh

import (
	"crypto/sha256"
	"fmt"

	"github.com/mezonai/mmn/logx"
)

func VerifyEntries(prev [32]byte, entries []Entry, slot uint64) error {
	logx.Info("POH", fmt.Sprintf("VerifyEntries: prev hash=%x slot=%d", prev, slot))
	logx.Info("POH", fmt.Sprintf("VerifyEntries: verifying %d entries in slot=%d", len(entries), slot))

	// Validate entries count bounds
	if len(entries) > MAX_ENTRIES_PER_SLOT {
		return fmt.Errorf("too many entries in slot %d: %d > %d", slot, len(entries), MAX_ENTRIES_PER_SLOT)
	}

	cur := prev

	for i, e := range entries {
		if err := ValidateEntry(e); err != nil {
			return fmt.Errorf("invalid entry %d in slot %d: %v", i, slot, err)
		}

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
			return fmt.Errorf("PoH mismatch: entry=%d slot=%d expected=%x got=%x", i, slot, e.Hash, cur)
		}
	}
	return nil
}

func ValidateEntry(e Entry) error {
	if e.NumHashes > MAX_NUM_HASHES {
		return fmt.Errorf("NumHashes too large: %d > %d", e.NumHashes, MAX_NUM_HASHES)
	}

	if len(e.Transactions) > MAX_TRANSACTIONS_PER_ENTRY {
		return fmt.Errorf("too many transactions: %d > %d", len(e.Transactions), MAX_TRANSACTIONS_PER_ENTRY)
	}

	for i, tx := range e.Transactions {
		if tx == nil {
			return fmt.Errorf("transaction %d is nil", i)
		}

		if len(tx.Sender) == 0 {
			return fmt.Errorf("transaction %d has empty sender", i)
		}

		if len(tx.Recipient) == 0 {
			return fmt.Errorf("transaction %d has empty recipient", i)
		}

		txSize := len(tx.Bytes())
		if txSize > MAX_ENTRY_SIZE/MAX_TRANSACTIONS_PER_ENTRY {
			return fmt.Errorf("transaction %d too large: %d bytes", i, txSize)
		}
	}

	entrySize := estimateEntrySize(e)
	if entrySize > MAX_ENTRY_SIZE {
		return fmt.Errorf("entry too large: %d bytes > %d", entrySize, MAX_ENTRY_SIZE)
	}

	return nil
}
