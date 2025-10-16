package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/security/abuse"
)

type RateLimiterConfig struct {
	MaxRequests     int
	WindowSize      time.Duration
	CleanupInterval time.Duration
}

func DefaultConfig() *RateLimiterConfig {
	return &RateLimiterConfig{
		MaxRequests:     100,
		WindowSize:      time.Second,
		CleanupInterval: 5 * time.Minute, // cleanup every 5 minutes
	}
}

type RequestEntry struct {
	Timestamp time.Time
}

type RateLimiterData struct {
	mu           sync.RWMutex
	currentCount int
	windowStart  time.Time
	lastClean    time.Time
}

type RateLimiter struct {
	config      *RateLimiterConfig
	requests    map[string]*RateLimiterData
	mu          sync.RWMutex
	stopCleanup chan struct{}
}

func NewRateLimiter(config *RateLimiterConfig) *RateLimiter {
	if config == nil {
		config = DefaultConfig()
	}

	rl := &RateLimiter{
		config:      config,
		requests:    make(map[string]*RateLimiterData),
		stopCleanup: make(chan struct{}),
	}

	go rl.cleanupExpiredEntries()

	return rl
}

// AllowWithContext checks if a request from the given key is allowed with context
func (rl *RateLimiter) AllowWithContext(ctx context.Context, key string) bool {
	now := time.Now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	data, exists := rl.requests[key]
	if !exists {
		data = &RateLimiterData{
			windowStart: now,
			lastClean:   now,
		}
		rl.requests[key] = data
	}

	data.mu.Lock()
	defer data.mu.Unlock()

	if now.Sub(data.windowStart) >= rl.config.WindowSize {
		data.currentCount = 0
		data.windowStart = now
	}

	if data.currentCount >= rl.config.MaxRequests {
		return false
	}

	data.currentCount++
	return true
}

func (rl *RateLimiter) cleanupExpiredEntries() {
	ticker := time.NewTicker(rl.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCleanup:
			return
		}
	}
}

func (rl *RateLimiter) cleanup() {
	now := time.Now()
	cutoff := now.Add(-rl.config.WindowSize)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	for key, data := range rl.requests {
		if rl.cleanupData(data, cutoff) {
			delete(rl.requests, key)
		}
	}
}

func (rl *RateLimiter) cleanupData(data *RateLimiterData, cutoff time.Time) bool {
	data.mu.Lock()
	defer data.mu.Unlock()

	if data.currentCount == 0 {
		return data.lastClean.Before(cutoff)
	}

	data.lastClean = time.Now()
	return false
}

func (rl *RateLimiter) Stop() {
	close(rl.stopCleanup)
}

type GlobalRateLimiter struct {
	ipLimiter     *RateLimiter
	walletLimiter *RateLimiter
	abuseDetector *abuse.AbuseDetector
	mu            sync.RWMutex
}

type GlobalRateLimiterConfig struct {
	IPConfig     *RateLimiterConfig
	WalletConfig *RateLimiterConfig
}

func DefaultGlobalConfig() *GlobalRateLimiterConfig {
	return &GlobalRateLimiterConfig{
		IPConfig: &RateLimiterConfig{
			MaxRequests:     100,
			WindowSize:      time.Second,
			CleanupInterval: 5 * time.Minute,
		},
		WalletConfig: &RateLimiterConfig{
			MaxRequests:     100,
			WindowSize:      time.Second,
			CleanupInterval: 5 * time.Minute,
		},
	}
}

func NewGlobalRateLimiterWithAbuseDetector(config *GlobalRateLimiterConfig, abuseDetector *abuse.AbuseDetector) *GlobalRateLimiter {
	if config == nil {
		config = DefaultGlobalConfig()
	}

	return &GlobalRateLimiter{
		ipLimiter:     NewRateLimiter(config.IPConfig),
		walletLimiter: NewRateLimiter(config.WalletConfig),
		abuseDetector: abuseDetector,
	}
}
func (grl *GlobalRateLimiter) AllowIPWithContext(ctx context.Context, ip string) bool {
	grl.mu.RLock()
	defer grl.mu.RUnlock()

	if grl.abuseDetector.IsIPBlacklisted(ip) {
		logx.Warn("SECURITY", "Alert abuse spam from IP:", ip)
	}

	return grl.ipLimiter.AllowWithContext(ctx, ip)
}

func (grl *GlobalRateLimiter) AllowWalletWithContext(ctx context.Context, wallet string) bool {
	grl.mu.RLock()
	defer grl.mu.RUnlock()
	if grl.abuseDetector.IsWalletBlacklisted(wallet) {
		logx.Warn("SECURITY", "Alert abuse spam from wallet:", wallet)
	}

	return grl.walletLimiter.AllowWithContext(ctx, wallet)
}

func (grl *GlobalRateLimiter) RecordTransaction(ip, wallet string) {
	grl.mu.RLock()
	defer grl.mu.RUnlock()
	grl.abuseDetector.TrackTransaction(ip, wallet)
}

func (grl *GlobalRateLimiter) Stop() {
	grl.mu.Lock()
	defer grl.mu.Unlock()
	grl.ipLimiter.Stop()
	grl.walletLimiter.Stop()
}
