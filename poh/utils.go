package poh

import (
	"crypto/sha256"
)

func GenerateTickOnlyEntries(seed [32]byte, numEntries int, hashesPerTick uint64) []Entry {
	if numEntries <= 0 || hashesPerTick == 0 {
		return nil
	}

	entries := make([]Entry, 0, numEntries)
	cur := seed

	for i := 0; i < numEntries; i++ {
		for n := uint64(1); n < hashesPerTick; n++ {
			cur = sha256.Sum256(cur[:])
		}

		cur = sha256.Sum256(cur[:])

		e := NewTickEntry(hashesPerTick, cur)
		entries = append(entries, e)
	}

	return entries
}
