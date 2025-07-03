package poh

import (
	"time"
)

// PohService runs PoH ticks at a fixed interval and flushes entries via callback.
// It handles both recorded transaction entries and tick-only entries.
// OnEntry will be called for each new Entry generated or recorded.

type PohService struct {
	Recorder     *PohRecorder
	TickInterval time.Duration
	OnEntry      func(entry Entry)
	stopCh       chan struct{}
}

// NewPohService creates a new service with the given recorder and tick interval.
func NewPohService(recorder *PohRecorder, interval time.Duration) *PohService {
	return &PohService{
		Recorder:     recorder,
		TickInterval: interval,
		stopCh:       make(chan struct{}),
	}
}

// Start begins the background goroutine emitting ticks and flushing entries.
func (s *PohService) Start() {
	ticker := time.NewTicker(s.TickInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.tickAndFlush()
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop signals the service to cease emitting ticks.
func (s *PohService) Stop() {
	close(s.stopCh)
}

// tickAndFlush generates a new tick entry (if due) and flushes all pending entries.
func (s *PohService) tickAndFlush() {
	// First, drain any recorded transaction entries.
	entries := s.Recorder.DrainEntries()

	// Then perform a tick; append the tick entry if one was produced.
	if tickEntry := s.Recorder.Tick(); tickEntry != nil {
		entries = append(entries, *tickEntry)
	}

	// Invoke callback for all flushed entries.
	for _, entry := range entries {
		if s.OnEntry != nil {
			s.OnEntry(entry)
		}
	}
}
