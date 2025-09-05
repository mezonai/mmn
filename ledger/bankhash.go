package ledger

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"

	"github.com/mezonai/mmn/types"
)

// ComputeAccountsDeltaHash computes a deterministic hash over a set of updated accounts.
// Each record is encoded as: len(address)|address|balance(8B BE)|nonce(8B BE)
// Accounts are sorted by address for determinism.
func ComputeAccountsDeltaHash(updated map[string]*types.Account) [32]byte {
	if len(updated) == 0 {
		return [32]byte{}
	}
	h := sha256.New()

	addresses := make([]string, 0, len(updated))
	for addr := range updated {
		addresses = append(addresses, addr)
	}
	sort.Strings(addresses)

	buf := make([]byte, 8)
	for _, addr := range addresses {
		acc := updated[addr]
		binary.BigEndian.PutUint64(buf, uint64(len(addr)))
		h.Write(buf)
		h.Write([]byte(addr))
		binary.BigEndian.PutUint64(buf, acc.Balance.Uint64())
		h.Write(buf)
		binary.BigEndian.PutUint64(buf, acc.Nonce)
		h.Write(buf)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// CombineBankHash combines previous bank hash and delta hash to produce new bank hash.
// new = SHA256(prev || delta). If prev is zero, returns delta.
func CombineBankHash(prev [32]byte, delta [32]byte) [32]byte {
	if isZeroHash(prev) {
		return delta
	}
	h := sha256.New()
	h.Write(prev[:])
	h.Write(delta[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func isZeroHash(h [32]byte) bool {
	for _, b := range h {
		if b != 0 {
			return false
		}
	}
	return true
}
