package abuse

import (
	"time"

	"github.com/mezonai/mmn/exception"
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

	exception.SafeGo("RateTrackerCleanup", func() {
		rt.cleanupRoutine()
	})

	return rt
}

func (rt *RateTracker) TrackIPRequest(ip string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.trackRequest(rt.ipRates, ip)
}

func (rt *RateTracker) TrackWalletRequest(wallet string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.trackRequest(rt.walletRates, wallet)
}

func (rt *RateTracker) trackRequest(rates map[string]*RateData, key string) {
	data, exists := rates[key]
	if !exists {
		data = &RateData{
			minuteStart: time.Now(),
			hourStart:   time.Now(),
			dayStart:    time.Now(),
			lastClean:   time.Now(),
		}
		rates[key] = data
	}

	data.mu.Lock()
	defer data.mu.Unlock()

	now := time.Now()

	// Reset counters if window has passed
	if now.Sub(data.minuteStart) >= time.Minute {
		data.minuteCount = 0
		data.minuteStart = now
	}
	if now.Sub(data.hourStart) >= time.Hour {
		data.hourCount = 0
		data.hourStart = now
	}
	if now.Sub(data.dayStart) >= 24*time.Hour {
		data.dayCount = 0
		data.dayStart = now
	}

	data.minuteCount++
	data.hourCount++
	data.dayCount++
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

	switch {
	case window <= time.Minute:
		return data.minuteCount
	case window <= time.Hour:
		return data.hourCount
	case window <= 24*time.Hour:
		return data.dayCount
	default:
		return 0
	}
}

func (rt *RateTracker) GetAllIPs() map[string]*RateData {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	result := make(map[string]*RateData)
	for ip, data := range rt.ipRates {
		result[ip] = data
	}
	return result
}

func (rt *RateTracker) GetAllWallets() map[string]*RateData {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	result := make(map[string]*RateData)
	for wallet, data := range rt.walletRates {
		result[wallet] = data
	}
	return result
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
	cutoff := now.Add(-rt.config.DayWindow)

	for ip, data := range rt.ipRates {
		if rt.cleanupData(data, cutoff) {
			delete(rt.ipRates, ip)
		}
	}

	for wallet, data := range rt.walletRates {
		if rt.cleanupData(data, cutoff) {
			delete(rt.walletRates, wallet)
		}
	}
}

func (rt *RateTracker) cleanupData(data *RateData, cutoff time.Time) bool {
	data.mu.Lock()
	defer data.mu.Unlock()

	if data.minuteCount == 0 && data.hourCount == 0 && data.dayCount == 0 {
		return data.lastClean.Before(cutoff)
	}

	data.lastClean = time.Now()
	return false
}
