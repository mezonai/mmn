package poh

import (
	"errors"
	"sort"
)

// LeaderScheduleEntry defines the leader assignment for a contiguous slot range.
// StartSlot and EndSlot are inclusive.
type LeaderScheduleEntry struct {
	StartSlot uint64 // first slot in the range
	EndSlot   uint64 // last slot in the range
	Leader    string // validator pubkey responsible for these slots
}

// LeaderSchedule maintains an ordered, non-overlapping set of schedule entries.
type LeaderSchedule struct {
	entries []LeaderScheduleEntry
}

// NewLeaderSchedule constructs a schedule and validates entries (sorted, non-overlapping).
func NewLeaderSchedule(entries []LeaderScheduleEntry) (*LeaderSchedule, error) {
	ls := &LeaderSchedule{entries: entries}
	if err := ls.Validate(); err != nil {
		return nil, err
	}
	return ls, nil
}

// LeaderAt returns the leader for a given slot, or false if none assigned.
func (ls *LeaderSchedule) LeaderAt(slot uint64) (string, bool) {
	// binary search since entries sorted by StartSlot
	i := sort.Search(len(ls.entries), func(i int) bool {
		return ls.entries[i].StartSlot > slot
	})
	// candidate index is i-1
	if i > 0 {
		e := ls.entries[i-1]
		if slot >= e.StartSlot && slot <= e.EndSlot {
			return e.Leader, true
		}
	}
	return "", false
}

// LeadersInRange returns all schedule entries overlapping [startSlot, endSlot].
func (ls *LeaderSchedule) LeadersInRange(startSlot, endSlot uint64) []LeaderScheduleEntry {
	var result []LeaderScheduleEntry
	for _, e := range ls.entries {
		if e.EndSlot < startSlot || e.StartSlot > endSlot {
			continue
		}
		result = append(result, e)
	}
	return result
}

// AddEntry appends a new entry and keeps entries sorted; user should Validate afterwards.
func (ls *LeaderSchedule) AddEntry(entry LeaderScheduleEntry) {
	ls.entries = append(ls.entries, entry)
	sort.Slice(ls.entries, func(i, j int) bool {
		return ls.entries[i].StartSlot < ls.entries[j].StartSlot
	})
}

// Validate ensures entries are sorted by StartSlot and non-overlapping.
func (ls *LeaderSchedule) Validate() error {
	if len(ls.entries) == 0 {
		return nil
	}
	// sort by StartSlot
	sort.Slice(ls.entries, func(i, j int) bool {
		return ls.entries[i].StartSlot < ls.entries[j].StartSlot
	})
	// check for overlaps
	for i := 1; i < len(ls.entries); i++ {
		prev := ls.entries[i-1]
		curr := ls.entries[i]
		if curr.StartSlot <= prev.EndSlot {
			return errors.New("overlapping schedule entries detected")
		}
	}
	return nil
}

// Entries returns a copy of the underlying schedule entries.
// It preserves immutability of the internal slice while allowing callers
// to consume the real, validated schedule ranges.
func (ls *LeaderSchedule) Entries() []LeaderScheduleEntry {
	if len(ls.entries) == 0 {
		return nil
	}
	out := make([]LeaderScheduleEntry, len(ls.entries))
	copy(out, ls.entries)
	return out
}
