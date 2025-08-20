package p2p

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type RateLimitConfig struct {
	// Authentication rate limits
	MaxAuthAttemptsPerMinute int
	MaxAuthFailuresPerHour   int
	AuthCooldownPeriod       time.Duration

	// Stream creation rate limits
	MaxStreamsPerMinute  int
	MaxStreamsPerPeer    int
	StreamCooldownPeriod time.Duration

	// Bandwidth rate limits
	MaxBytesPerSecond int64
	MaxBytesPerMinute int64
	MaxBytesPerHour   int64
	BurstAllowance    int64

	// Message rate limits
	MaxMessageSize       int
	MaxTotalMessageSize  int64
	LargeMessageCooldown time.Duration

	// Block sync rate limits
	MaxBlockRequestsPerMinute int
	MaxBlockSizePerRequest    int64
	SyncCooldownPeriod        time.Duration

	// Transaction rate limits
	MaxTxPerSecond        int
	MaxTxPerMinute        int
	MaxInvalidTxPerMinute int
	TxCooldownPeriod      time.Duration

	// Connection rate limits
	MaxConnectionsPerMinute  int
	MaxConnectionsPerPeer    int
	ConnectionCooldownPeriod time.Duration
}

func DefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		// Authentication limits
		MaxAuthAttemptsPerMinute: 5,
		MaxAuthFailuresPerHour:   10,
		AuthCooldownPeriod:       1 * time.Minute,

		// Stream limits
		MaxStreamsPerMinute:  20,
		MaxStreamsPerPeer:    5,
		StreamCooldownPeriod: 30 * time.Second,

		// Bandwidth limits
		MaxBytesPerSecond: 1024 * 1024,            // 1MB/s
		MaxBytesPerMinute: 50 * 1024 * 1024,       // 50MB/min
		MaxBytesPerHour:   1 * 1024 * 1024 * 1024, // 1GB/hour
		BurstAllowance:    5 * 1024 * 1024,        // 5MB burst

		// Message limits
		MaxMessageSize:       2 * 1024,   // 2KB
		MaxTotalMessageSize:  100 * 1024, // 100KB/min
		LargeMessageCooldown: 10 * time.Second,

		// Block sync limits
		MaxBlockRequestsPerMinute: 10,
		MaxBlockSizePerRequest:    100,
		SyncCooldownPeriod:        30 * time.Second,

		// Transaction limits
		MaxTxPerSecond:        100,
		MaxTxPerMinute:        5000,
		MaxInvalidTxPerMinute: 100,
		TxCooldownPeriod:      5 * time.Second,

		// Connection limits
		MaxConnectionsPerMinute:  5,
		MaxConnectionsPerPeer:    3,
		ConnectionCooldownPeriod: 60 * time.Second,
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
	peerID               peer.ID
	config               *RateLimitConfig
	authLimiter          *SlidingWindowCounter
	streamLimiter        *SlidingWindowCounter
	bandwidthLimiter     *RateLimit
	messageLimiter       *SlidingWindowCounter
	blockSyncLimiter     *SlidingWindowCounter
	txLimiter            *SlidingWindowCounter
	connectionLimiter    *SlidingWindowCounter
	authCooldown         time.Time
	streamCooldown       time.Time
	largeMessageCooldown time.Time
	syncCooldown         time.Time
	txCooldown           time.Time
	connectionCooldown   time.Time

	mu sync.RWMutex
}

func NewPeerRateLimiter(peerID peer.ID, config *RateLimitConfig) *PeerRateLimiter {
	return &PeerRateLimiter{
		peerID:            peerID,
		config:            config,
		authLimiter:       NewSlidingWindowCounter(1, time.Minute, config.MaxAuthAttemptsPerMinute),
		streamLimiter:     NewSlidingWindowCounter(1, time.Minute, config.MaxStreamsPerMinute),
		bandwidthLimiter:  NewRateLimit(int(config.MaxBytesPerSecond), int(config.MaxBytesPerSecond)),
		messageLimiter:    NewSlidingWindowCounter(1, time.Minute, int(config.MaxTotalMessageSize/1024)),
		blockSyncLimiter:  NewSlidingWindowCounter(1, time.Minute, config.MaxBlockRequestsPerMinute),
		txLimiter:         NewSlidingWindowCounter(1, time.Minute, config.MaxTxPerMinute),
		connectionLimiter: NewSlidingWindowCounter(1, time.Minute, config.MaxConnectionsPerMinute),
	}
}

func (prl *PeerRateLimiter) CheckAuthRateLimit() bool {
	prl.mu.Lock()
	defer prl.mu.Unlock()

	if time.Now().Before(prl.authCooldown) {
		return false
	}

	return prl.authLimiter.Increment()
}

func (prl *PeerRateLimiter) CheckStreamRateLimit() bool {
	prl.mu.Lock()
	defer prl.mu.Unlock()

	if time.Now().Before(prl.streamCooldown) {
		return false
	}

	return prl.streamLimiter.Increment()
}

func (prl *PeerRateLimiter) CheckBandwidthRateLimit(bytes int64) bool {
	prl.mu.RLock()
	defer prl.mu.RUnlock()

	if bytes > int64(prl.config.MaxMessageSize) {
		return false
	}

	return prl.bandwidthLimiter.Take(int(bytes))
}

func (prl *PeerRateLimiter) CheckMessageRateLimit(messageSize int) bool {
	prl.mu.Lock()
	defer prl.mu.Unlock()

	if messageSize > prl.config.MaxMessageSize {
		if time.Now().Before(prl.largeMessageCooldown) {
			return false
		}
		prl.largeMessageCooldown = time.Now().Add(prl.config.LargeMessageCooldown)
	}

	return prl.messageLimiter.Increment()
}

func (prl *PeerRateLimiter) CheckBlockSyncRateLimit() bool {
	prl.mu.Lock()
	defer prl.mu.Unlock()

	if time.Now().Before(prl.syncCooldown) {
		return false
	}

	return prl.blockSyncLimiter.Increment()
}

func (prl *PeerRateLimiter) CheckTransactionRateLimit() bool {
	prl.mu.Lock()
	defer prl.mu.Unlock()

	if time.Now().Before(prl.txCooldown) {
		return false
	}

	return prl.txLimiter.Increment()
}

func (prl *PeerRateLimiter) CheckConnectionRateLimit() bool {
	prl.mu.Lock()
	defer prl.mu.Unlock()

	if time.Now().Before(prl.connectionCooldown) {
		return false
	}

	return prl.connectionLimiter.Increment()
}

func (prl *PeerRateLimiter) GetRateLimitStatus() map[string]interface{} {
	prl.mu.RLock()
	defer prl.mu.RUnlock()

	return map[string]interface{}{
		"auth_attempts":       prl.authLimiter.GetCurrentCount(),
		"streams_created":     prl.streamLimiter.GetCurrentCount(),
		"bandwidth_used":      prl.bandwidthLimiter.Counts,
		"messages_sent":       prl.messageLimiter.GetCurrentCount(),
		"block_sync_requests": prl.blockSyncLimiter.GetCurrentCount(),
		"transactions_sent":   prl.txLimiter.GetCurrentCount(),
		"connections":         prl.connectionLimiter.GetCurrentCount(),
		"auth_cooldown":       prl.authCooldown,
		"stream_cooldown":     prl.streamCooldown,
		"tx_cooldown":         prl.txCooldown,
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
