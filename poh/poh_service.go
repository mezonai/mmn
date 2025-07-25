package poh

import (
	"fmt"
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
	go s.tickAndFlush()
}

func (s *PohService) Stop() {
	close(s.stopCh)
}

func (s *PohService) tickAndFlush() {
	fmt.Println("Ticking and flushing")
	ticker := time.NewTicker(s.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			entries := s.Recorder.DrainEntries()
			if tickEntry := s.Recorder.Tick(); tickEntry != nil {
				entries = append(entries, *tickEntry)
			}
			for _, entry := range entries {
				if s.OnEntry != nil {
					s.OnEntry(entry)
				}
			}
		case <-s.stopCh:
			fmt.Println("Ticking and flushing stop")
			return
		}
	}
}
