package bankhash

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"

	"github.com/mezonai/mmn/types"
)

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
		val := acc.Balance.Uint64()
		binary.BigEndian.PutUint64(buf, val)
		h.Write(buf)
		binary.BigEndian.PutUint64(buf, acc.Nonce)
		h.Write(buf)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

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
