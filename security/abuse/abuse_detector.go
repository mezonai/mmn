package abuse

import (
	"fmt"
	"time"

	"github.com/mezonai/mmn/logx"
)

func DefaultAbuseConfig() *AbuseConfig {
	return &AbuseConfig{
		MaxTxPerMinute: 1,     // 10 tx per second
		MaxTxPerHour:   36000,  // 10 tx per second for 1 hour
		MaxTxPerDay:    864000, // 10 tx per second for 1 day

		AutoBlacklistTxPerMinute: 2, // 20 tx per second
	}
}

func NewAbuseDetector(config *AbuseConfig) *AbuseDetector {
	if config == nil {
		config = DefaultAbuseConfig()
	}

	ad := &AbuseDetector{
		rateTracker:    NewRateTracker(nil),
		config:         config,
		flaggedIPs:     make(map[string]*AbuseFlag),
		flaggedWallets: make(map[string]*AbuseFlag),
		metrics:        &AbuseMetrics{},
	}

	go ad.startBackgroundMonitoring()

	return ad
}

func (ad *AbuseDetector) CheckTransactionRate(ip, wallet string) error {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	ad.rateTracker.TrackTransaction(ip, wallet)

	if err := ad.checkIPTransactionRate(ip); err != nil {
		return err
	}

	if err := ad.checkWalletTransactionRate(wallet); err != nil {
		return err
	}

	return nil
}

func (ad *AbuseDetector) checkIPTransactionRate(ip string) error {
	minuteRate := ad.rateTracker.GetIPRate(ip, time.Minute)
	hourRate := ad.rateTracker.GetIPRate(ip, time.Hour)
	dayRate := ad.rateTracker.GetIPRate(ip, 24*time.Hour)

	// Check for auto-blacklist (only block at very high rates)
	if minuteRate >= ad.config.AutoBlacklistTxPerMinute {
		ad.flagAbuse(ip, "ip", fmt.Sprintf("Auto-blacklist: %d tx/min (limit: %d)", minuteRate, ad.config.AutoBlacklistTxPerMinute))
		ad.flaggedIPs[ip].IsBlacklisted = true
		ad.metrics.AutoBlacklists++
		return fmt.Errorf("IP %s auto-blacklisted: %d tx/min", ip, minuteRate)
	}

	if minuteRate > ad.config.MaxTxPerMinute || hourRate > ad.config.MaxTxPerHour || dayRate > ad.config.MaxTxPerDay {
		reason := fmt.Sprintf("High tx rate: %d/min, %d/hour, %d/day (limits: %d/%d/%d)",
			minuteRate, hourRate, dayRate, ad.config.MaxTxPerMinute, ad.config.MaxTxPerHour, ad.config.MaxTxPerDay)
		ad.flagAbuse(ip, "ip", reason)
	}

	return nil
}

func (ad *AbuseDetector) checkWalletTransactionRate(wallet string) error {
	minuteRate := ad.rateTracker.GetWalletRate(wallet, time.Minute)
	hourRate := ad.rateTracker.GetWalletRate(wallet, time.Hour)
	dayRate := ad.rateTracker.GetWalletRate(wallet, 24*time.Hour)

	// Check for flagging (but don't block - just log for monitoring)
	if minuteRate > ad.config.MaxTxPerMinute || hourRate > ad.config.MaxTxPerHour || dayRate > ad.config.MaxTxPerDay {
		reason := fmt.Sprintf("High tx rate: %d/min, %d/hour, %d/day (limits: %d/%d/%d)",
			minuteRate, hourRate, dayRate, ad.config.MaxTxPerMinute, ad.config.MaxTxPerHour, ad.config.MaxTxPerDay)
		ad.flagAbuse(wallet, "wallet", reason)
	}

	return nil
}

// flagAbuse flags an entity for abusive behavior
func (ad *AbuseDetector) flagAbuse(entity, entityType, reason string) {
	now := time.Now()

	switch entityType {
case IP:
		flag, exists := ad.flaggedIPs[entity]
		if exists {
			// Update existing flag
			flag.LastSeen = now
			flag.Count++
			flag.Reason = reason
		} else {
			// Create new flag
			ad.flaggedIPs[entity] = &AbuseFlag{
				Entity:        entity,
				EntityType:    entityType,
				Reason:        reason,
				FirstSeen:     now,
				LastSeen:      now,
				Count:         1,
				IsBlacklisted: false,
			}
			ad.metrics.TotalFlags++
		}

		logx.Warn("ABUSE", fmt.Sprintf("Flagged IP %s: %s (count: %d)", entity, reason, ad.flaggedIPs[entity].Count))
	case WALLET:
		flag, exists := ad.flaggedWallets[entity]
		if exists {
			// Update existing flag
			flag.LastSeen = now
			flag.Count++
			flag.Reason = reason
		} else {
			// Create new flag
			ad.flaggedWallets[entity] = &AbuseFlag{
				Entity:        entity,
				EntityType:    entityType,
				Reason:        reason,
				FirstSeen:     now,
				LastSeen:      now,
				Count:         1,
				IsBlacklisted: false,
			}
			ad.metrics.TotalFlags++
		}

		logx.Warn("ABUSE", fmt.Sprintf("Flagged wallet %s: %s (count: %d)", entity, reason, ad.flaggedWallets[entity].Count))
	}
}

// GetFlaggedIPs returns all flagged IPs
func (ad *AbuseDetector) GetFlaggedIPs() map[string]*AbuseFlag {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	result := make(map[string]*AbuseFlag)
	for ip, flag := range ad.flaggedIPs {
		result[ip] = flag
	}
	return result
}

// GetFlaggedWallets returns all flagged wallets
func (ad *AbuseDetector) GetFlaggedWallets() map[string]*AbuseFlag {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	result := make(map[string]*AbuseFlag)
	for wallet, flag := range ad.flaggedWallets {
		result[wallet] = flag
	}
	return result
}

// GetMetrics returns current metrics
func (ad *AbuseDetector) GetMetrics() *AbuseMetrics {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	ad.metrics.CurrentFlags = len(ad.flaggedIPs) + len(ad.flaggedWallets)
	ad.metrics.CurrentBlacklists = 0

	for _, flag := range ad.flaggedIPs {
		if flag.IsBlacklisted {
			ad.metrics.CurrentBlacklists++
		}
	}
	for _, flag := range ad.flaggedWallets {
		if flag.IsBlacklisted {
			ad.metrics.CurrentBlacklists++
		}
	}

	return ad.metrics
}

// GetRateStats returns current rate statistics
func (ad *AbuseDetector) GetRateStats() *RateStats {
	return ad.rateTracker.GetStats()
}

func (ad *AbuseDetector) IsIPBlacklisted(ip string) bool {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	flag, exists := ad.flaggedIPs[ip]
	return exists && flag.IsBlacklisted
}

func (ad *AbuseDetector) IsWalletBlacklisted(wallet string) bool {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	flag, exists := ad.flaggedWallets[wallet]
	return exists && flag.IsBlacklisted
}

func (ad *AbuseDetector) TrackTransaction(ip, wallet string) {
	ad.rateTracker.TrackTransaction(ip, wallet)
}

func (ad *AbuseDetector) startBackgroundMonitoring() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		ad.performBackgroundChecks()
	}
}

func (ad *AbuseDetector) performBackgroundChecks() {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	now := time.Now()

	for ip, flag := range ad.flaggedIPs {
		if flag.IsBlacklisted {
			continue
		}

		minuteRate := ad.rateTracker.GetIPRate(ip, time.Minute)
		hourRate := ad.rateTracker.GetIPRate(ip, time.Hour)
		dayRate := ad.rateTracker.GetIPRate(ip, 24*time.Hour)

		if minuteRate >= ad.config.AutoBlacklistTxPerMinute {
			ad.autoBlacklistIP(ip, fmt.Sprintf("Auto-blacklist: %d tx/min (limit: %d)", minuteRate, ad.config.AutoBlacklistTxPerMinute))
		} else if hourRate >= ad.config.MaxTxPerHour || dayRate >= ad.config.MaxTxPerDay {
			reason := fmt.Sprintf("High long-term rate: %d/min, %d/hour, %d/day", minuteRate, hourRate, dayRate)
			ad.flagAbuse(ip, "ip", reason)
		}
	}

	for wallet, flag := range ad.flaggedWallets {
		if flag.IsBlacklisted {
			continue
		}

		minuteRate := ad.rateTracker.GetWalletRate(wallet, time.Minute)
		hourRate := ad.rateTracker.GetWalletRate(wallet, time.Hour)
		dayRate := ad.rateTracker.GetWalletRate(wallet, 24*time.Hour)

		if minuteRate >= ad.config.AutoBlacklistTxPerMinute {
			ad.autoBlacklistWallet(wallet, fmt.Sprintf("Auto-blacklist: %d tx/min (limit: %d)", minuteRate, ad.config.AutoBlacklistTxPerMinute))
		} else if hourRate >= ad.config.MaxTxPerHour || dayRate >= ad.config.MaxTxPerDay {
			reason := fmt.Sprintf("High long-term rate: %d/min, %d/hour, %d/day", minuteRate, hourRate, dayRate)
			ad.flagAbuse(wallet, "wallet", reason)
		}
	}

	cutoff := now.Add(-24 * time.Hour)
	ad.cleanupOldFlags(cutoff)
}

func (ad *AbuseDetector) autoBlacklistIP(ip, reason string) {
	flag, exists := ad.flaggedIPs[ip]
	if !exists {
		flag = &AbuseFlag{
			Entity:        ip,
			EntityType:    "ip",
			Reason:        reason,
			FirstSeen:     time.Now(),
			LastSeen:      time.Now(),
			Count:         1,
			IsBlacklisted: true,
		}
		ad.flaggedIPs[ip] = flag
		ad.metrics.TotalFlags++
	} else {
		flag.IsBlacklisted = true
		flag.Reason = reason
		flag.LastSeen = time.Now()
	}

	ad.metrics.AutoBlacklists++
	logx.Warn("ABUSE", fmt.Sprintf("Auto-blacklisted IP %s: %s", ip, reason))
}

func (ad *AbuseDetector) autoBlacklistWallet(wallet, reason string) {
	flag, exists := ad.flaggedWallets[wallet]
	if !exists {
		flag = &AbuseFlag{
			Entity:        wallet,
			EntityType:    "wallet",
			Reason:        reason,
			FirstSeen:     time.Now(),
			LastSeen:      time.Now(),
			Count:         1,
			IsBlacklisted: true,
		}
		ad.flaggedWallets[wallet] = flag
		ad.metrics.TotalFlags++
	} else {
		flag.IsBlacklisted = true
		flag.Reason = reason
		flag.LastSeen = time.Now()
	}

	ad.metrics.AutoBlacklists++
	logx.Warn("ABUSE", fmt.Sprintf("Auto-blacklisted wallet %s: %s", wallet, reason))
}

func (ad *AbuseDetector) cleanupOldFlags(cutoff time.Time) {
	for ip, flag := range ad.flaggedIPs {
		if flag.LastSeen.Before(cutoff) && !flag.IsBlacklisted {
			delete(ad.flaggedIPs, ip)
		}
	}

	for wallet, flag := range ad.flaggedWallets {
		if flag.LastSeen.Before(cutoff) && !flag.IsBlacklisted {
			delete(ad.flaggedWallets, wallet)
		}
	}
}
