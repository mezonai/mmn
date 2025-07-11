package poh

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

var LOW_POWER_MODE = ^uint64(0) // max uint64

type PohEntry struct {
	NumHashes uint64
	Hash      [32]byte
	Tick      bool
}

type Poh struct {
	Hash            [32]byte
	NumHashes       uint64
	HashesPerTick   uint64
	RemainingHashes uint64
	TickNumber      uint64
	SlotStartTime   time.Time

	mu     sync.Mutex
	stopCh chan struct{}
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
		stopCh:          make(chan struct{}),
	}
}

func (p *Poh) Stop() {
	close(p.stopCh)
}

func (p *Poh) hashOnce(hash []byte) {
	p.Hash = sha256.Sum256(hash)
	p.NumHashes++
	p.RemainingHashes--
}

func (p *Poh) Record(mixin [32]byte) *PohEntry {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.RemainingHashes <= 1 {
		return nil
	}

	p.hashOnce(append(p.Hash[:], mixin[:]...))

	entry := &PohEntry{
		NumHashes: p.NumHashes,
		Hash:      p.Hash,
		Tick:      false,
	}
	return entry
}

func (p *Poh) Tick() *PohEntry {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.RemainingHashes != 1 {
		return nil
	}

	p.hashOnce(p.Hash[:])

	entry := &PohEntry{
		NumHashes: p.NumHashes,
		Hash:      p.Hash,
		Tick:      true,
	}

	p.RemainingHashes = p.HashesPerTick
	p.NumHashes = 0
	p.TickNumber += 1
	return entry
}

func (p *Poh) Run() {
	go p.AutoHash()
}

func (p *Poh) AutoHash() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.mu.Lock()
			fmt.Println("RemainingHashes", p.RemainingHashes)
			if p.RemainingHashes > 1 {
				p.hashOnce(p.Hash[:])
			}
			p.mu.Unlock()
		}
	}
}
