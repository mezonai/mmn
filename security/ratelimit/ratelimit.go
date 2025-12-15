package ratelimit

import (
	"sync"
	"time"

	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/security/abuse"
	"golang.org/x/time/rate"
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
	limiter  *rate.Limiter
	lastSeen time.Time
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

	exception.SafeGo("StartCleanup", func() {
		rl.cleanupExpiredEntries()
	})

	return rl
}

// AllowWithContext checks if a request from the given key is allowed with context
func (rl *RateLimiter) IsAllowed(key string) bool {
	now := time.Now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	data, exists := rl.requests[key]
	if !exists {
		limiter := rate.NewLimiter(rate.Every(rl.config.WindowSize/time.Duration(rl.config.MaxRequests)), rl.config.MaxRequests)
		data = &RateLimiterData{
			limiter:  limiter,
			lastSeen: now,
		}
		rl.requests[key] = data
	}

	data.lastSeen = now
	return data.limiter.Allow()
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
	ttl := rl.config.WindowSize * 3
	cutoff := now.Add(-ttl)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	for key, data := range rl.requests {
		if rl.cleanupData(data, cutoff) {
			delete(rl.requests, key)
		}
	}
}

func (rl *RateLimiter) cleanupData(data *RateLimiterData, cutoff time.Time) bool {
	return data.lastSeen.Before(cutoff)
}

func (rl *RateLimiter) Stop() {
	close(rl.stopCleanup)
}

type GlobalRateLimiter struct {
	ipLimiter     *RateLimiter
	abuseDetector *abuse.AbuseDetector
	mu            sync.RWMutex
}

type GlobalRateLimiterConfig struct {
	IPConfig *RateLimiterConfig
}

func DefaultGlobalConfig() *GlobalRateLimiterConfig {
	return &GlobalRateLimiterConfig{
		IPConfig: &RateLimiterConfig{
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
		abuseDetector: abuseDetector,
	}
}
func (grl *GlobalRateLimiter) IsIPAllowed(ip string) bool {
	grl.mu.RLock()
	defer grl.mu.RUnlock()

	if grl.abuseDetector.IsIPBlacklisted(ip) {
		logx.Warn("SECURITY", "Alert abuse spam from IP:", ip)
	}

	return grl.ipLimiter.IsAllowed(ip)
}

func (grl *GlobalRateLimiter) TrackIPRequest(ip string) {
	grl.abuseDetector.TrackIPRequest(ip)
}

func (grl *GlobalRateLimiter) TrackWalletRequest(wallet string) {
	grl.abuseDetector.TrackWalletRequest(wallet)
}

func (grl *GlobalRateLimiter) Stop() {
	grl.mu.Lock()
	defer grl.mu.Unlock()
	grl.ipLimiter.Stop()
}
