package p2p

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type RateLimitConfig struct {
	// Authentication rate limits
	MaxAuthAttemptsPerMinute int

	// Stream creation rate limits
	MaxStreamsPerMinute int

	// Bandwidth rate limits
	MaxBytesPerSecond int64

	// Block sync rate limits
	MaxBlockRequestsPerMinute int
}

func DefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		// Authentication limits
		MaxAuthAttemptsPerMinute: 5,

		// Stream limits
		MaxStreamsPerMinute: 20,

		// Bandwidth limits
		MaxBytesPerSecond: 2 * 1024 * 1024,

		// Block sync limits
		MaxBlockRequestsPerMinute: 45,
	}
}

type RateLimit struct {
	Counts     int
	MaxCounts  int
	RefillRate int
	LastRefill time.Time
	mu         sync.Mutex
}

func NewRateLimit(maxCounts, refillRate int) *RateLimit {
	return &RateLimit{
		Counts:     maxCounts,
		MaxCounts:  maxCounts,
		RefillRate: refillRate,
		LastRefill: time.Now(),
	}
}

func (tb *RateLimit) Take(tokens int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.LastRefill).Seconds()
	tb.Counts = min(tb.MaxCounts, tb.Counts+int(elapsed*float64(tb.RefillRate)))
	tb.LastRefill = now

	if tb.Counts >= tokens {
		tb.Counts -= tokens
		return true
	}
	return false
}

type SlidingWindowCounter struct {
	Windows      []int
	CurrentIndex int
	WindowSize   time.Duration
	MaxCount     int
	LastUpdate   time.Time
	mu           sync.Mutex
}

func NewSlidingWindowCounter(windowCount int, windowSize time.Duration, maxCount int) *SlidingWindowCounter {
	windows := make([]int, windowCount)
	return &SlidingWindowCounter{
		Windows:    windows,
		WindowSize: windowSize,
		MaxCount:   maxCount,
		LastUpdate: time.Now(),
	}
}

func (swc *SlidingWindowCounter) Increment() bool {
	swc.mu.Lock()
	defer swc.mu.Unlock()

	now := time.Now()
	if now.Sub(swc.LastUpdate) >= swc.WindowSize {
		swc.advanceWindow()
	}

	if swc.Windows[swc.CurrentIndex] < swc.MaxCount {
		swc.Windows[swc.CurrentIndex]++
		return true
	}
	return false
}

func (swc *SlidingWindowCounter) advanceWindow() {
	swc.CurrentIndex = (swc.CurrentIndex + 1) % len(swc.Windows)
	swc.Windows[swc.CurrentIndex] = 0
	swc.LastUpdate = time.Now()
}

func (swc *SlidingWindowCounter) GetCurrentCount() int {
	swc.mu.Lock()
	defer swc.mu.Unlock()
	return swc.Windows[swc.CurrentIndex]
}

type PeerRateLimiter struct {
	peerID           peer.ID
	config           *RateLimitConfig
	authLimiter      *SlidingWindowCounter
	streamLimiter    *SlidingWindowCounter
	bandwidthLimiter *RateLimit
	blockSyncLimiter *SlidingWindowCounter

	mu sync.RWMutex
}

func NewPeerRateLimiter(peerID peer.ID, config *RateLimitConfig) *PeerRateLimiter {
	return &PeerRateLimiter{
		peerID:           peerID,
		config:           config,
		authLimiter:      NewSlidingWindowCounter(1, time.Minute, config.MaxAuthAttemptsPerMinute),
		streamLimiter:    NewSlidingWindowCounter(1, time.Minute, config.MaxStreamsPerMinute),
		bandwidthLimiter: NewRateLimit(int(config.MaxBytesPerSecond), int(config.MaxBytesPerSecond)),
		blockSyncLimiter: NewSlidingWindowCounter(1, time.Minute, config.MaxBlockRequestsPerMinute),
	}
}

func (prl *PeerRateLimiter) CheckAuthRateLimit() bool {
	prl.mu.Lock()
	defer prl.mu.Unlock()
	return prl.authLimiter.Increment()
}

func (prl *PeerRateLimiter) CheckStreamRateLimit() bool {
	prl.mu.Lock()
	defer prl.mu.Unlock()
	return prl.streamLimiter.Increment()
}

func (prl *PeerRateLimiter) CheckBandwidthRateLimit(bytes int64) bool {
	prl.mu.RLock()
	defer prl.mu.RUnlock()
	return prl.bandwidthLimiter.Take(int(bytes))
}

func (prl *PeerRateLimiter) CheckBlockSyncRateLimit() bool {
	prl.mu.Lock()
	defer prl.mu.Unlock()
	return prl.blockSyncLimiter.Increment()
}

func (prl *PeerRateLimiter) GetRateLimitStatus() map[string]interface{} {
	prl.mu.RLock()
	defer prl.mu.RUnlock()

	return map[string]interface{}{
		"auth_attempts":       prl.authLimiter.GetCurrentCount(),
		"streams_created":     prl.streamLimiter.GetCurrentCount(),
		"bandwidth_used":      prl.bandwidthLimiter.Counts,
		"block_sync_requests": prl.blockSyncLimiter.GetCurrentCount(),
	}
}

type RateLimitManager struct {
	config       *RateLimitConfig
	peerLimiters map[peer.ID]*PeerRateLimiter
	mu           sync.RWMutex
}

func NewRateLimitManager(config *RateLimitConfig) *RateLimitManager {
	if config == nil {
		config = DefaultRateLimitConfig()
	}

	return &RateLimitManager{
		config:       config,
		peerLimiters: make(map[peer.ID]*PeerRateLimiter),
	}
}

func (rlm *RateLimitManager) GetPeerRateLimiter(peerID peer.ID) *PeerRateLimiter {
	rlm.mu.Lock()
	defer rlm.mu.Unlock()

	if limiter, exists := rlm.peerLimiters[peerID]; exists {
		return limiter
	}

	limiter := NewPeerRateLimiter(peerID, rlm.config)
	rlm.peerLimiters[peerID] = limiter
	return limiter
}

func (rlm *RateLimitManager) GetGlobalRateLimitStatus() map[peer.ID]map[string]interface{} {
	rlm.mu.RLock()
	defer rlm.mu.RUnlock()

	status := make(map[peer.ID]map[string]interface{})
	for peerID, limiter := range rlm.peerLimiters {
		status[peerID] = limiter.GetRateLimitStatus()
	}

	return status
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
