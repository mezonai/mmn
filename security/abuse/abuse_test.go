package abuse

import (
	"strings"
	"testing"
)

func newTestDetector(t *testing.T) *AbuseDetector {
	t.Helper()
	cfg := &AbuseConfig{
		MaxTxPerMinute:             2,
		MaxTxPerHour:               100000,
		MaxTxPerDay:                100000,
		AutoBlacklistTxPerMinute:   999999,
	}
	return NewAbuseDetector(cfg)
}

func TestIPFlaggingWhenExceedingMaxTxPerMinute(t *testing.T) {
	ad := newTestDetector(t)
	ip := "1.2.3.4"
	wallet := "wallet-abc"

	// 3 tx in the same minute, MaxTxPerMinute=2 -> should flag IP, not blacklist, no error
	for i := 0; i < 3; i++ {
		if err := ad.CheckTransactionRate(ip, wallet); err != nil {
			t.Fatalf("unexpected error, should only flag: %v", err)
		}
	}

	ips := ad.GetFlaggedIPs()
	flag, ok := ips[ip]
	if !ok {
		t.Fatalf("expected IP %s to be flagged", ip)
	}
	if flag.IsBlacklisted {
		t.Fatalf("expected IP %s to NOT be blacklisted", ip)
	}
	if !strings.Contains(flag.Reason, "High tx rate") {
		t.Fatalf("expected flag reason to contain 'High tx rate', got: %s", flag.Reason)
	}
}

func TestIPAutoBlacklistWhenExceedingAutoBlacklistTxPerMinute(t *testing.T) {
	cfg := &AbuseConfig{
		MaxTxPerMinute:             100000,
		MaxTxPerHour:               100000,
		MaxTxPerDay:                100000,
		AutoBlacklistTxPerMinute:   3,
	}
	ad := NewAbuseDetector(cfg)

	ip := "5.6.7.8"
	wallet := "wallet-def"

	// On the third tx, minuteRate >= 3 triggers auto-blacklist and returns error
	if err := ad.CheckTransactionRate(ip, wallet); err != nil {
		t.Fatalf("unexpected error on first tx: %v", err)
	}
	if err := ad.CheckTransactionRate(ip, wallet); err != nil {
		t.Fatalf("unexpected error on second tx: %v", err)
	}
	if err := ad.CheckTransactionRate(ip, wallet); err == nil {
		t.Fatalf("expected error due to auto-blacklist on third tx")
	}

	ips := ad.GetFlaggedIPs()
	flag, ok := ips[ip]
	if !ok {
		t.Fatalf("expected IP %s to be present in flagged list", ip)
	}
	if !flag.IsBlacklisted {
		t.Fatalf("expected IP %s to be blacklisted", ip)
	}

	metrics := ad.GetMetrics()
	if metrics.AutoBlacklists < 1 {
		t.Fatalf("expected AutoBlacklists >= 1, got %d", metrics.AutoBlacklists)
	}
}

func TestWalletFlaggingOnTransactions(t *testing.T) {
	ad := newTestDetector(t)
	ip := "9.9.9.9"
	wallet := "wallet-xyz"

	// Make wallet exceed per-minute threshold (MaxTxPerMinute=2 in test config)
	for i := 0; i < 3; i++ {
		if err := ad.CheckTransactionRate(ip, wallet); err != nil {
			t.Fatalf("unexpected error (should only flag): %v", err)
		}
	}

	wallets := ad.GetFlaggedWallets()
	flag, ok := wallets[wallet]
	if !ok {
		t.Fatalf("expected wallet %s to be flagged", wallet)
	}
	if flag.IsBlacklisted {
		t.Fatalf("expected wallet %s to NOT be blacklisted", wallet)
	}
	if !strings.Contains(flag.Reason, "High tx rate") {
		t.Fatalf("expected wallet flag reason to contain 'High tx rate', got: %s", flag.Reason)
	}
}

