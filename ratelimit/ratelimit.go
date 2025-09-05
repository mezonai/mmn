package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type RateLimiterConfig struct {
	MaxRequests     int
	WindowSize      time.Duration
	CleanupInterval time.Duration
}

func DefaultConfig() *RateLimiterConfig {
	return &RateLimiterConfig{
		MaxRequests:     10,
		WindowSize:      time.Second,
		CleanupInterval: 5 * time.Minute, // cleanup every 5 minutes
	}
}

type RequestEntry struct {
	Timestamp time.Time
}

type RateLimiter struct {
	config      *RateLimiterConfig
	requests    map[string][]RequestEntry
	mu          sync.RWMutex
	stopCleanup chan struct{}
}

func NewRateLimiter(config *RateLimiterConfig) *RateLimiter {
	if config == nil {
		config = DefaultConfig()
	}

	rl := &RateLimiter{
		config:      config,
		requests:    make(map[string][]RequestEntry),
		stopCleanup: make(chan struct{}),
	}

	go rl.cleanupExpiredEntries()

	return rl
}

func (rl *RateLimiter) Allow(key string) bool {
	return rl.AllowWithContext(context.Background(), key)
}

// AllowWithContext checks if a request from the given key is allowed with context
func (rl *RateLimiter) AllowWithContext(ctx context.Context, key string) bool {
	now := time.Now()
	cutoff := now.Add(-rl.config.WindowSize)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	requests, exists := rl.requests[key]
	if !exists {
		requests = make([]RequestEntry, 0)
	}

	validRequests := make([]RequestEntry, 0)
	for _, req := range requests {
		if req.Timestamp.After(cutoff) {
			validRequests = append(validRequests, req)
		}
	}

	if len(validRequests) >= rl.config.MaxRequests {
		rl.requests[key] = validRequests
		return false
	}

	validRequests = append(validRequests, RequestEntry{Timestamp: now})
	rl.requests[key] = validRequests

	return true
}

func (rl *RateLimiter) GetStats(key string) (int, time.Time) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	requests, exists := rl.requests[key]
	if !exists {
		return 0, time.Time{}
	}

	now := time.Now()
	cutoff := now.Add(-rl.config.WindowSize)

	validCount := 0
	var oldestRequest time.Time

	for _, req := range requests {
		if req.Timestamp.After(cutoff) {
			validCount++
			if oldestRequest.IsZero() || req.Timestamp.Before(oldestRequest) {
				oldestRequest = req.Timestamp
			}
		}
	}

	return validCount, oldestRequest
}

func (rl *RateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.requests, key)
}

func (rl *RateLimiter) ResetAll() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.requests = make(map[string][]RequestEntry)
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

	for key, requests := range rl.requests {
		validRequests := make([]RequestEntry, 0)
		for _, req := range requests {
			if req.Timestamp.After(cutoff) {
				validRequests = append(validRequests, req)
			}
		}

		if len(validRequests) == 0 {
			delete(rl.requests, key)
		} else {
			rl.requests[key] = validRequests
		}
	}
}

func (rl *RateLimiter) Stop() {
	close(rl.stopCleanup)
}

func (rl *RateLimiter) GetConfig() *RateLimiterConfig {
	return rl.config
}

func (rl *RateLimiter) UpdateConfig(config *RateLimiterConfig) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.config = config
}

type GlobalRateLimiter struct {
	ipLimiter     *RateLimiter
	walletLimiter *RateLimiter
	globalLimiter *RateLimiter
	mu            sync.RWMutex
}

type GlobalRateLimiterConfig struct {
	IPConfig     *RateLimiterConfig
	WalletConfig *RateLimiterConfig
	GlobalConfig *RateLimiterConfig
}

func DefaultGlobalConfig() *GlobalRateLimiterConfig {
	return &GlobalRateLimiterConfig{
		IPConfig: &RateLimiterConfig{
			MaxRequests:     10,
			WindowSize:      time.Second,
			CleanupInterval: 5 * time.Minute,
		},
		WalletConfig: &RateLimiterConfig{
			MaxRequests:     10,
			WindowSize:      time.Second,
			CleanupInterval: 5 * time.Minute,
		},
		GlobalConfig: &RateLimiterConfig{
			MaxRequests:     1000,
			WindowSize:      time.Second,
			CleanupInterval: 5 * time.Minute,
		},
	}
}

func NewGlobalRateLimiter(config *GlobalRateLimiterConfig) *GlobalRateLimiter {
	if config == nil {
		config = DefaultGlobalConfig()
	}

	return &GlobalRateLimiter{
		ipLimiter:     NewRateLimiter(config.IPConfig),
		walletLimiter: NewRateLimiter(config.WalletConfig),
		globalLimiter: NewRateLimiter(config.GlobalConfig),
	}
}

func (grl *GlobalRateLimiter) AllowIP(ip string) bool {
	return grl.AllowIPWithContext(context.Background(), ip)
}

func (grl *GlobalRateLimiter) AllowIPWithContext(ctx context.Context, ip string) bool {
	grl.mu.RLock()
	defer grl.mu.RUnlock()

	return grl.ipLimiter.AllowWithContext(ctx, ip)
}

func (grl *GlobalRateLimiter) AllowWallet(wallet string) bool {
	return grl.AllowWalletWithContext(context.Background(), wallet)
}

func (grl *GlobalRateLimiter) AllowWalletWithContext(ctx context.Context, wallet string) bool {
	grl.mu.RLock()
	defer grl.mu.RUnlock()

	return grl.walletLimiter.AllowWithContext(ctx, wallet)
}

func (grl *GlobalRateLimiter) AllowGlobal() bool {
	return grl.AllowGlobalWithContext(context.Background())
}

func (grl *GlobalRateLimiter) AllowGlobalWithContext(ctx context.Context) bool {
	grl.mu.RLock()
	defer grl.mu.RUnlock()

	return grl.globalLimiter.AllowWithContext(ctx, "global")
}

func (grl *GlobalRateLimiter) AllowAll(ip, wallet string) bool {
	return grl.AllowAllWithContext(context.Background(), ip, wallet)
}

func (grl *GlobalRateLimiter) AllowAllWithContext(ctx context.Context, ip, wallet string) bool {
	grl.mu.RLock()
	defer grl.mu.RUnlock()

	if !grl.ipLimiter.AllowWithContext(ctx, ip) {
		return false
	}

	if !grl.walletLimiter.AllowWithContext(ctx, wallet) {
		return false
	}

	if !grl.globalLimiter.AllowWithContext(ctx, "global") {
		return false
	}

	return true
}

func (grl *GlobalRateLimiter) GetStats(ip, wallet string) (map[string]interface{}, error) {
	grl.mu.RLock()
	defer grl.mu.RUnlock()

	ipCount, ipOldest := grl.ipLimiter.GetStats(ip)
	walletCount, walletOldest := grl.walletLimiter.GetStats(wallet)
	globalCount, globalOldest := grl.globalLimiter.GetStats("global")

	return map[string]interface{}{
		"ip": map[string]interface{}{
			"count":  ipCount,
			"oldest": ipOldest,
		},
		"wallet": map[string]interface{}{
			"count":  walletCount,
			"oldest": walletOldest,
		},
		"global": map[string]interface{}{
			"count":  globalCount,
			"oldest": globalOldest,
		},
	}, nil
}

func (grl *GlobalRateLimiter) ResetIP(ip string) {
	grl.mu.Lock()
	defer grl.mu.Unlock()
	grl.ipLimiter.Reset(ip)
}

func (grl *GlobalRateLimiter) ResetWallet(wallet string) {
	grl.mu.Lock()
	defer grl.mu.Unlock()
	grl.walletLimiter.Reset(wallet)
}

func (grl *GlobalRateLimiter) ResetAll() {
	grl.mu.Lock()
	defer grl.mu.Unlock()
	grl.ipLimiter.ResetAll()
	grl.walletLimiter.ResetAll()
	grl.globalLimiter.ResetAll()
}

func (grl *GlobalRateLimiter) Stop() {
	grl.mu.Lock()
	defer grl.mu.Unlock()
	grl.ipLimiter.Stop()
	grl.walletLimiter.Stop()
	grl.globalLimiter.Stop()
}

type RateLimitError struct {
	Type    string
	Key     string
	Message string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded for %s '%s': %s", e.Type, e.Key, e.Message)
}

func NewRateLimitError(rateType, key, message string) *RateLimitError {
	return &RateLimitError{
		Type:    rateType,
		Key:     key,
		Message: message,
	}
}
