package store

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/mezonai/mmn/db"
)

// StateMetaStore stores auxiliary state metadata like bank hashes per slot.
// This is intentionally separate from consensus-critical block data.
// Keys:
// - PrefixBankHashBySlot + <8-byte big-endian slot> => 32-byte bank hash

type StateMetaStore interface {
	SetBankHash(slot uint64, bankHash [32]byte) error
	GetBankHash(slot uint64) ([32]byte, bool, error)
}

type GenericStateMetaStore struct {
	provider db.DatabaseProvider
}

func NewGenericStateMetaStore(provider db.DatabaseProvider) *GenericStateMetaStore {
	return &GenericStateMetaStore{provider: provider}
}

func (s *GenericStateMetaStore) slotToBankHashKey(slot uint64) []byte {
	key := make([]byte, len(PrefixBankHashBySlot)+8)
	copy(key, PrefixBankHashBySlot)
	binary.BigEndian.PutUint64(key[len(PrefixBankHashBySlot):], slot)
	return key
}

func (s *GenericStateMetaStore) SetBankHash(slot uint64, bankHash [32]byte) error {
	key := s.slotToBankHashKey(slot)
	value := bankHash[:]
	if err := s.provider.Put(key, value); err != nil {
		return fmt.Errorf("failed to store bank hash for slot %d: %w", slot, err)
	}
	return nil
}

func (s *GenericStateMetaStore) GetBankHash(slot uint64) ([32]byte, bool, error) {
	key := s.slotToBankHashKey(slot)
	value, err := s.provider.Get(key)
	if err != nil {
		return [32]byte{}, false, fmt.Errorf("failed to get bank hash for slot %d: %w", slot, err)
	}
	if value == nil || len(value) == 0 {
		return [32]byte{}, false, nil
	}
	if len(value) != sha256.Size {
		return [32]byte{}, false, fmt.Errorf("invalid bank hash length: %d", len(value))
	}
	var out [32]byte
	copy(out[:], value)
	return out, true, nil
}
