package abuse

import (
	"sync"
	"time"
)

type RateTracker struct {
	mu          sync.RWMutex
	ipRates     map[string]*RateData
	walletRates map[string]*RateData
	config      *RateConfig
}

type RateData struct {
	mu sync.RWMutex

	minuteCount int
	hourCount   int
	dayCount    int

	minuteStart time.Time
	hourStart   time.Time
	dayStart    time.Time

	lastClean time.Time
}

type RateConfig struct {
	CleanupInterval time.Duration
	MinuteWindow    time.Duration
	HourWindow      time.Duration
	DayWindow       time.Duration
}

type RateStats struct {
	IPRates     map[string]map[string]int `json:"ip_rates"`
	WalletRates map[string]map[string]int `json:"wallet_rates"`
}

type AbuseDetector struct {
	mu sync.RWMutex

	rateTracker *RateTracker

	config *AbuseConfig

	flaggedIPs     map[string]*AbuseFlag
	flaggedWallets map[string]*AbuseFlag

	metrics *AbuseMetrics
}

type AbuseConfig struct {
	MaxTxPerMinute int
	MaxTxPerHour   int
	MaxTxPerDay    int

	AutoBlacklistTxPerMinute int
}

type AbuseFlag struct {
	Entity        string    `json:"entity"`      // IP or wallet address
	EntityType    string    `json:"entity_type"` // "ip" or "wallet"
	Reason        string    `json:"reason"`      // Reason for flagging
	FirstSeen     time.Time `json:"first_seen"`  // When first flagged
	LastSeen      time.Time `json:"last_seen"`   // When last seen
	Count         int       `json:"count"`       // Number of violations
	IsBlacklisted bool      `json:"is_blacklisted"`
}

type AbuseMetrics struct {
	TotalFlags        int64 `json:"total_flags"`
	AutoBlacklists    int64 `json:"auto_blacklists"`
	CurrentFlags      int   `json:"current_flags"`
	CurrentBlacklists int   `json:"current_blacklists"`
}
