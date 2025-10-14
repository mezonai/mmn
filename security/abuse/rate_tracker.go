package abuse

import (
	"time"
)

func DefaultRateConfig() *RateConfig {
	return &RateConfig{
		CleanupInterval: 5 * time.Minute,
		MinuteWindow:    time.Minute,
		HourWindow:      time.Hour,
		DayWindow:       24 * time.Hour,
	}
}

func NewRateTracker(config *RateConfig) *RateTracker {
	if config == nil {
		config = DefaultRateConfig()
	}

	rt := &RateTracker{
		ipRates:     make(map[string]*RateData),
		walletRates: make(map[string]*RateData),
		config:      config,
	}

	go rt.cleanupRoutine()

	return rt
}

func (rt *RateTracker) TrackTransaction(ip, wallet string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	// Track IP rate
	rt.trackRequest(rt.ipRates, ip)

	// Track wallet rate
	rt.trackRequest(rt.walletRates, wallet)
}

func (rt *RateTracker) TrackFaucet(ip, wallet string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	// Track IP rate
	rt.trackRequest(rt.ipRates, ip)

	// Track wallet rate
	rt.trackRequest(rt.walletRates, wallet)
}

func (rt *RateTracker) trackRequest(rates map[string]*RateData, key string) {
	data, exists := rates[key]
	if !exists {
		data = &RateData{
			requests:  make([]time.Time, 0),
			lastClean: time.Now(),
		}
		rates[key] = data
	}

	data.mu.Lock()
	data.requests = append(data.requests, time.Now())
	data.mu.Unlock()
}

func (rt *RateTracker) GetIPRate(ip string, window time.Duration) int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	data, exists := rt.ipRates[ip]
	if !exists {
		return 0
	}

	return rt.getRate(data, window)
}

// GetWalletRate returns current rate for a wallet
func (rt *RateTracker) GetWalletRate(wallet string, window time.Duration) int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	data, exists := rt.walletRates[wallet]
	if !exists {
		return 0
	}

	return rt.getRate(data, window)
}

func (rt *RateTracker) getRate(data *RateData, window time.Duration) int {
	data.mu.RLock()
	defer data.mu.RUnlock()

	now := time.Now()
	cutoff := now.Add(-window)

	count := 0
	for _, t := range data.requests {
		if t.After(cutoff) {
			count++
		}
	}

	return count
}

func (rt *RateTracker) GetStats() *RateStats {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	stats := &RateStats{
		IPRates:     make(map[string]map[string]int),
		WalletRates: make(map[string]map[string]int),
	}

	// Get IP rates
	for ip, data := range rt.ipRates {
		stats.IPRates[ip] = map[string]int{
			"minute": rt.getRate(data, rt.config.MinuteWindow),
			"hour":   rt.getRate(data, rt.config.HourWindow),
			"day":    rt.getRate(data, rt.config.DayWindow),
		}
	}

	// Get wallet rates
	for wallet, data := range rt.walletRates {
		stats.WalletRates[wallet] = map[string]int{
			"minute": rt.getRate(data, rt.config.MinuteWindow),
			"hour":   rt.getRate(data, rt.config.HourWindow),
			"day":    rt.getRate(data, rt.config.DayWindow),
		}
	}

	return stats
}

func (rt *RateTracker) cleanupRoutine() {
	ticker := time.NewTicker(rt.config.CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		rt.cleanup()
	}
}

func (rt *RateTracker) cleanup() {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rt.config.DayWindow) // Keep data for 1 day

	// Cleanup IP rates
	for ip, data := range rt.ipRates {
		if rt.cleanupData(data, cutoff) {
			delete(rt.ipRates, ip)
		}
	}

	// Cleanup wallet rates
	for wallet, data := range rt.walletRates {
		if rt.cleanupData(data, cutoff) {
			delete(rt.walletRates, wallet)
		}
	}
}

func (rt *RateTracker) cleanupData(data *RateData, cutoff time.Time) bool {
	data.mu.Lock()
	defer data.mu.Unlock()

	kept := data.requests[:0]
	for _, t := range data.requests {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	data.requests = kept

	return len(data.requests) == 0
}
