package poh

import (
	"time"

	"github.com/mezonai/mmn/logx"

	"github.com/mezonai/mmn/exception"
)

type PohService struct {
	Recorder     *PohRecorder
	TickInterval time.Duration
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
	exception.SafeGoWithPanic("stickAndFlush", func() {
		s.tickAndFlush()
	})
}

func (s *PohService) Stop() {
	close(s.stopCh)
}

func (s *PohService) tickAndFlush() {
	logx.Info("POH SERVICE", "Ticking and flushing")
	ticker := time.NewTicker(s.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.Recorder.Tick()
		case <-s.stopCh:
			logx.Info("POH SERVICE", "Ticking and flushing stopped")
			return
		}
	}
}
