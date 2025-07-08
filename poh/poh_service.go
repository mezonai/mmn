package poh

import (
	"time"
)

type PohService struct {
	Recorder     *PohRecorder
	TickInterval time.Duration
	OnEntry      func(entry Entry)
	stopCh       chan struct{}
}

func NewPohService(recorder *PohRecorder, interval time.Duration) *PohService {
	return &PohService{
		Recorder:     recorder,
		TickInterval: interval,
		stopCh:       make(chan struct{}),
	}
}

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

func (s *PohService) Stop() {
	close(s.stopCh)
}

func (s *PohService) tickAndFlush() {
	entries := s.Recorder.DrainEntries()

	if tickEntry := s.Recorder.Tick(); tickEntry != nil {
		entries = append(entries, *tickEntry)
	}

	for _, entry := range entries {
		if s.OnEntry != nil {
			s.OnEntry(entry)
		}
	}
}
