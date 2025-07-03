package poh

import (
	"crypto/sha256"
	"time"
)

var LOW_POWER_MODE = ^uint64(0) // max uint64

type PohEntry struct {
	NumHashes uint64
	Hash      [32]byte
}

type Poh struct {
	Hash            [32]byte
	NumHashes       uint64
	HashesPerTick   uint64
	RemainingHashes uint64
	TickNumber      uint64
	SlotStartTime   time.Time
}

func NewPoh(seed []byte, hashesPerTickOpt *uint64) *Poh {
	var hashesPerTick uint64
	if hashesPerTickOpt == nil {
		hashesPerTick = LOW_POWER_MODE
	} else {
		hashesPerTick = *hashesPerTickOpt
	}

	if hashesPerTick <= 1 {
		panic("hashesPerTick must be > 1")
	}

	return &Poh{
		Hash:            sha256.Sum256(seed),
		NumHashes:       0,
		HashesPerTick:   hashesPerTick,
		RemainingHashes: hashesPerTick,
		TickNumber:      0,
		SlotStartTime:   time.Now(),
	}
}

func (p *Poh) Reset(seed []byte, hashesPerTickOpt *uint64) {
	*p = *NewPoh(seed, hashesPerTickOpt)
}

func (p *Poh) HashesPerTickValue() uint64 {
	return p.HashesPerTick
}

func (p *Poh) TargetPohTime(targetNsPerTick uint64) time.Time {
	offsetTickNs := targetNsPerTick * p.TickNumber
	offsetNs := targetNsPerTick * p.NumHashes / p.HashesPerTick
	return p.SlotStartTime.Add(time.Duration(offsetNs+offsetTickNs) * time.Nanosecond)
}

// Return true if should tick next
func (p *Poh) HashN(maxNumHashes uint64) bool {
	if p.RemainingHashes <= 1 {
		return true
	}

	numHashes := min(p.RemainingHashes-1, maxNumHashes)
	for i := uint64(0); i < numHashes; i++ {
		p.Hash = sha256.Sum256(p.Hash[:])
	}

	p.NumHashes += numHashes
	p.RemainingHashes -= numHashes

	return p.RemainingHashes == 1
}

func (p *Poh) Record(mixin [32]byte) *PohEntry {
	if p.RemainingHashes == 1 {
		return nil
	}

	combined := append(p.Hash[:], mixin[:]...)
	p.Hash = sha256.Sum256(combined)

	entry := &PohEntry{
		NumHashes: p.NumHashes + 1,
		Hash:      p.Hash,
	}

	p.NumHashes = 0
	p.RemainingHashes -= 1
	return entry
}

func (p *Poh) Tick() *PohEntry {
	p.Hash = sha256.Sum256(p.Hash[:])
	p.NumHashes += 1
	p.RemainingHashes -= 1

	if p.HashesPerTick != LOW_POWER_MODE && p.RemainingHashes != 0 {
		return nil
	}

	entry := &PohEntry{
		NumHashes: p.NumHashes,
		Hash:      p.Hash,
	}

	p.RemainingHashes = p.HashesPerTick
	p.NumHashes = 0
	p.TickNumber += 1
	return entry
}

func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
