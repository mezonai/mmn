package snapshot

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/poh"
	"github.com/mezonai/mmn/types"
)

const accountPrefix = "account:"

// EpochMetadata contains epoch-related information
type EpochMetadata struct {
	EpochNumber    uint64 `json:"epoch_number"`
	EpochStartSlot uint64 `json:"epoch_start_slot"`
	EpochEndSlot   uint64 `json:"epoch_end_slot"`
	EpochDuration  uint64 `json:"epoch_duration"` // in slots
}

// SnapshotMeta holds snapshot header metadata
type SnapshotMeta struct {
	Slot           uint64                    `json:"slot"`
	BankHash       [32]byte                  `json:"bank_hash"`
	LeaderSchedule []poh.LeaderScheduleEntry `json:"leader_schedule"`
}

type SnapshotFile struct {
	Meta     SnapshotMeta    `json:"meta"`
	Accounts []types.Account `json:"accounts"`
}

func ComputeFullBankHash(provider db.DatabaseProvider) ([32]byte, error) {
	iterable, ok := provider.(db.IterableProvider)
	if !ok {
		return [32]byte{}, fmt.Errorf("provider does not support iteration")
	}

	type item struct {
		addr string
		acc  types.Account
	}
	items := make([]item, 0, 1024)
	err := iterable.IteratePrefix([]byte(accountPrefix), func(key, value []byte) bool {
		var acc types.Account
		if err := json.Unmarshal(value, &acc); err != nil {
			return false
		}
		items = append(items, item{addr: acc.Address, acc: acc})
		return true
	})
	if err != nil {
		return [32]byte{}, err
	}

	sort.Slice(items, func(i, j int) bool { return items[i].addr < items[j].addr })

	h := sha256.New()
	buf := make([]byte, 8)
	for _, it := range items {
		addr := it.addr
		acc := it.acc
		binary.BigEndian.PutUint64(buf, uint64(len(addr)))
		h.Write(buf)
		h.Write([]byte(addr))
		binary.BigEndian.PutUint64(buf, acc.Balance)
		h.Write(buf)
		binary.BigEndian.PutUint64(buf, acc.Nonce)
		h.Write(buf)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

// WriteSnapshot writes a full snapshot of all accounts with given slot and bank hash
func WriteSnapshot(dir string, provider db.DatabaseProvider, slot uint64, bankHash [32]byte, leaderSchedule []poh.LeaderScheduleEntry) (string, error) {
	iterable, ok := provider.(db.IterableProvider)
	if !ok {
		return "", fmt.Errorf("provider does not support iteration")
	}

	var accounts []types.Account
	err := iterable.IteratePrefix([]byte(accountPrefix), func(key, value []byte) bool {
		var acc types.Account
		if err := json.Unmarshal(value, &acc); err != nil {
			return false
		}
		accounts = append(accounts, acc)
		return true
	})
	if err != nil {
		return "", fmt.Errorf("iterate accounts: %w", err)
	}

	file := SnapshotFile{
		Meta: SnapshotMeta{
			Slot:           slot,
			BankHash:       bankHash,
			LeaderSchedule: leaderSchedule,
		},
		Accounts: accounts,
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal snapshot: %w", err)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("mkdir snapshot dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("snapshot-%d.json", slot))
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("write snapshot file: %w", err)
	}
	return path, nil
}

// ReadSnapshot loads a snapshot file from disk
func ReadSnapshot(path string) (*SnapshotFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s SnapshotFile
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// WriteSnapshotWithDefaults writes a snapshot with default values for epoch and leader schedule
func WriteSnapshotWithDefaults(dir string, provider db.DatabaseProvider, slot uint64, bankHash [32]byte, leaderSchedule []poh.LeaderScheduleEntry) (string, error) {
	return WriteSnapshot(dir, provider, slot, bankHash, leaderSchedule)
}
