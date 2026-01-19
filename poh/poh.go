package poh

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/logx"
)

var LowPowerMode = ^uint64(0) // max uint64

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
		hashesPerTick = LowPowerMode
	} else {
		hashesPerTick = *hashesPerTickOpt
	}

	if hashesPerTick <= 1 {
		panic("hashesPerTick must be > 1")
	}

	return &Poh{
		Hash:             sha256.Sum256(seed), // Poh recorder will reset the hash to the seed
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

	logx.Info("POH", fmt.Sprintf("PoH Reset: seed %x", seed))
	p.Hash = seed
	p.RemainingHashes = p.HashesPerTick
	p.NumHashes = 0
	p.SlotStartTime = time.Now()
}

func (p *Poh) Stop() {
	close(p.stopCh)
}

func (p *Poh) HashOnce(hash []byte) {
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

	p.HashOnce(append(p.Hash[:], mixin[:]...))

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

	p.HashOnce(p.Hash[:])

	entry := &PohEntry{
		NumHashes: p.NumHashes,
		Hash:      p.Hash,
		Tick:      true,
	}

	p.RemainingHashes = p.HashesPerTick
	p.NumHashes = 0
	return entry
}

func (p *Poh) TickFastForward(seenHash [32]byte, fromTick, toTick uint64) [32]byte {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Hash = seenHash
	numHashes := (toTick - fromTick) * p.HashesPerTick
	for i := uint64(0); i < numHashes; i++ {
		p.Hash = sha256.Sum256(p.Hash[:])
	}
	p.NumHashes = 0
	p.RemainingHashes = p.HashesPerTick
	return p.Hash
}

func (p *Poh) Run() {
	exception.SafeGoWithPanic("AutoHash", func() {
		p.AutoHash()
	})
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
			if p.RemainingHashes > 1 {
				p.HashOnce(p.Hash[:])
			}
			p.mu.Unlock()
		}
	}
}
