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
	Hash             [32]byte
	NumHashes        uint64
	HashesPerTick    uint64
	RemainingHashes  uint64
	SlotStartTime    time.Time
	autoHashInterval time.Duration

	mu     sync.Mutex
	stopCh chan struct{}
}

func NewPoh(seed []byte, hashesPerTickOpt *uint64, autoHashInterval time.Duration) *Poh {
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
		Hash:             sha256.Sum256(seed),
		NumHashes:        0,
		HashesPerTick:    hashesPerTick,
		RemainingHashes:  hashesPerTick,
		SlotStartTime:    time.Now(),
		stopCh:           make(chan struct{}),
		autoHashInterval: autoHashInterval,
	}
}

func (p *Poh) Reset(seed [32]byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	fmt.Println("PoH Reset: seed", seed)
	p.Hash = seed
	p.RemainingHashes = p.HashesPerTick
	p.NumHashes = 0
	p.SlotStartTime = time.Now()
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

	p.NumHashes = 0
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
	return entry
}

func (p *Poh) RecordTick() *PohEntry {
	remaining := p.HashesPerTick - p.NumHashes
	for i := uint64(0); i < remaining; i++ {
		p.Hash = sha256.Sum256(p.Hash[:])
	}
	entry := &PohEntry{
		Hash:      p.Hash,
		NumHashes: remaining,
		Tick:      true,
	}

	p.NumHashes = 0
	p.RemainingHashes = p.HashesPerTick
	return entry
}

func (p *Poh) Run() {
	go p.AutoHash()
}

func (p *Poh) AutoHash() {
	ticker := time.NewTicker(p.autoHashInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.mu.Lock()
			fmt.Println("AutoHash: RemainingHashes", p.RemainingHashes)
			if p.RemainingHashes > 1 {
				p.hashOnce(p.Hash[:])
			}
			p.mu.Unlock()
		}
	}
}
