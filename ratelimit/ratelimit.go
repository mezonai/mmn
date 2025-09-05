package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RateLimiterConfig holds configuration for rate limiting
type RateLimiterConfig struct {
	MaxRequests     int           // Maximum number of requests allowed
	WindowSize      time.Duration // Time window for rate limiting
	CleanupInterval time.Duration // How often to clean up expired entries
}

// DefaultConfig returns a default configuration
func DefaultConfig() *RateLimiterConfig {
	return &RateLimiterConfig{
		MaxRequests:     10,              // 10 requests
		WindowSize:      time.Second,     // per second
		CleanupInterval: 5 * time.Minute, // cleanup every 5 minutes
	}
}

// RequestEntry represents a single request timestamp
type RequestEntry struct {
	Timestamp time.Time
}

// RateLimiter implements sliding window rate limiting
type RateLimiter struct {
	config      *RateLimiterConfig
	requests    map[string][]RequestEntry // key -> list of request timestamps
	mu          sync.RWMutex
	stopCleanup chan struct{}
}

// NewRateLimiter creates a new rate limiter with the given configuration
func NewRateLimiter(config *RateLimiterConfig) *RateLimiter {
	if config == nil {
		config = DefaultConfig()
	}

	rl := &RateLimiter{
		config:      config,
		requests:    make(map[string][]RequestEntry),
		stopCleanup: make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanupExpiredEntries()

	return rl
}

// Allow checks if a request from the given key is allowed
func (rl *RateLimiter) Allow(key string) bool {
	return rl.AllowWithContext(context.Background(), key)
}

// AllowWithContext checks if a request from the given key is allowed with context
func (rl *RateLimiter) AllowWithContext(ctx context.Context, key string) bool {
	now := time.Now()
	cutoff := now.Add(-rl.config.WindowSize)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Get or create request list for this key
	requests, exists := rl.requests[key]
	if !exists {
		requests = make([]RequestEntry, 0)
	}

	// Remove expired requests (older than window size)
	validRequests := make([]RequestEntry, 0)
	for _, req := range requests {
		if req.Timestamp.After(cutoff) {
			validRequests = append(validRequests, req)
		}
	}

	// Check if we're under the limit
	if len(validRequests) >= rl.config.MaxRequests {
		rl.requests[key] = validRequests
		return false
	}

	// Add current request
	validRequests = append(validRequests, RequestEntry{Timestamp: now})
	rl.requests[key] = validRequests

	return true
}

// GetStats returns statistics for a given key
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

// Reset removes all entries for a given key
func (rl *RateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.requests, key)
}

// ResetAll removes all entries
func (rl *RateLimiter) ResetAll() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.requests = make(map[string][]RequestEntry)
}

// cleanupExpiredEntries periodically removes expired entries to prevent memory leaks
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

// cleanup removes expired entries
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

// Stop stops the cleanup goroutine
func (rl *RateLimiter) Stop() {
	close(rl.stopCleanup)
}

// GetConfig returns the current configuration
func (rl *RateLimiter) GetConfig() *RateLimiterConfig {
	return rl.config
}

// UpdateConfig updates the rate limiter configuration
func (rl *RateLimiter) UpdateConfig(config *RateLimiterConfig) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.config = config
}

// GlobalRateLimiter wraps multiple rate limiters for different types
type GlobalRateLimiter struct {
	ipLimiter     *RateLimiter
	walletLimiter *RateLimiter
	globalLimiter *RateLimiter
	mu            sync.RWMutex
}

// GlobalRateLimiterConfig holds configuration for global rate limiting
type GlobalRateLimiterConfig struct {
	IPConfig     *RateLimiterConfig
	WalletConfig *RateLimiterConfig
	GlobalConfig *RateLimiterConfig
}

// DefaultGlobalConfig returns a default global configuration
func DefaultGlobalConfig() *GlobalRateLimiterConfig {
	return &GlobalRateLimiterConfig{
		IPConfig: &RateLimiterConfig{
			MaxRequests:     50, // 50 requests per IP per second (increased for testing)
			WindowSize:      time.Second,
			CleanupInterval: 5 * time.Minute,
		},
		WalletConfig: &RateLimiterConfig{
			MaxRequests:     30, // 30 requests per wallet per second (increased for testing)
			WindowSize:      time.Second,
			CleanupInterval: 5 * time.Minute,
		},
		GlobalConfig: &RateLimiterConfig{
			MaxRequests:     1000, // 1000 total requests per second
			WindowSize:      time.Second,
			CleanupInterval: 5 * time.Minute,
		},
	}
}

// NewGlobalRateLimiter creates a new global rate limiter
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

// AllowIP checks if a request from the given IP is allowed
func (grl *GlobalRateLimiter) AllowIP(ip string) bool {
	return grl.AllowIPWithContext(context.Background(), ip)
}

// AllowIPWithContext checks if a request from the given IP is allowed with context
func (grl *GlobalRateLimiter) AllowIPWithContext(ctx context.Context, ip string) bool {
	grl.mu.RLock()
	defer grl.mu.RUnlock()

	return grl.ipLimiter.AllowWithContext(ctx, ip)
}

// AllowWallet checks if a request from the given wallet is allowed
func (grl *GlobalRateLimiter) AllowWallet(wallet string) bool {
	return grl.AllowWalletWithContext(context.Background(), wallet)
}

// AllowWalletWithContext checks if a request from the given wallet is allowed with context
func (grl *GlobalRateLimiter) AllowWalletWithContext(ctx context.Context, wallet string) bool {
	grl.mu.RLock()
	defer grl.mu.RUnlock()

	return grl.walletLimiter.AllowWithContext(ctx, wallet)
}

// AllowGlobal checks if a global request is allowed
func (grl *GlobalRateLimiter) AllowGlobal() bool {
	return grl.AllowGlobalWithContext(context.Background())
}

// AllowGlobalWithContext checks if a global request is allowed with context
func (grl *GlobalRateLimiter) AllowGlobalWithContext(ctx context.Context) bool {
	grl.mu.RLock()
	defer grl.mu.RUnlock()

	return grl.globalLimiter.AllowWithContext(ctx, "global")
}

// AllowAll checks if a request is allowed for IP, wallet, and global limits
func (grl *GlobalRateLimiter) AllowAll(ip, wallet string) bool {
	return grl.AllowAllWithContext(context.Background(), ip, wallet)
}

// AllowAllWithContext checks if a request is allowed for IP, wallet, and global limits with context
func (grl *GlobalRateLimiter) AllowAllWithContext(ctx context.Context, ip, wallet string) bool {
	grl.mu.RLock()
	defer grl.mu.RUnlock()

	// Check all three limits
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

// GetStats returns statistics for all limiters
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

// ResetIP resets the IP rate limiter for the given IP
func (grl *GlobalRateLimiter) ResetIP(ip string) {
	grl.mu.Lock()
	defer grl.mu.Unlock()
	grl.ipLimiter.Reset(ip)
}

// ResetWallet resets the wallet rate limiter for the given wallet
func (grl *GlobalRateLimiter) ResetWallet(wallet string) {
	grl.mu.Lock()
	defer grl.mu.Unlock()
	grl.walletLimiter.Reset(wallet)
}

// ResetAll resets all rate limiters
func (grl *GlobalRateLimiter) ResetAll() {
	grl.mu.Lock()
	defer grl.mu.Unlock()
	grl.ipLimiter.ResetAll()
	grl.walletLimiter.ResetAll()
	grl.globalLimiter.ResetAll()
}

// Stop stops all rate limiters
func (grl *GlobalRateLimiter) Stop() {
	grl.mu.Lock()
	defer grl.mu.Unlock()
	grl.ipLimiter.Stop()
	grl.walletLimiter.Stop()
	grl.globalLimiter.Stop()
}

// RateLimitError represents a rate limit error
type RateLimitError struct {
	Type    string
	Key     string
	Message string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded for %s '%s': %s", e.Type, e.Key, e.Message)
}

// NewRateLimitError creates a new rate limit error
func NewRateLimitError(rateType, key, message string) *RateLimitError {
	return &RateLimitError{
		Type:    rateType,
		Key:     key,
		Message: message,
	}
}
