package p2p

import (
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mezonai/mmn/logx"
)

type PeerScore struct {
	PeerID          peer.ID
	Score           float64
	LastUpdated     time.Time
	ConnectionCount int
	AuthFailures    int
	ValidBlocks     int
	InvalidBlocks   int
	ValidTxs        int
	InvalidTxs      int
	Uptime          time.Duration
	ResponseTime    time.Duration
	BandwidthUsage  int64
	LastSeen        time.Time
}

type PeerScoringConfig struct {
	MinScoreForAllowlist   float64
	MaxScoreForBlacklist   float64
	ScoreDecayRate         float64
	AuthFailurePenalty     float64
	ValidBlockBonus        float64
	InvalidBlockPenalty    float64
	ValidTxBonus           float64
	InvalidTxPenalty       float64
	UptimeBonus            float64
	ResponseTimeBonus      float64
	BandwidthPenalty       float64
	AutoAllowlistThreshold float64
	AutoBlacklistThreshold float64
	ScoreUpdateInterval    time.Duration
	RateLimitConfig        *RateLimitConfig
}

func DefaultPeerScoringConfig() *PeerScoringConfig {
	return &PeerScoringConfig{
		MinScoreForAllowlist:   50.0,
		MaxScoreForBlacklist:   10.0,
		ScoreDecayRate:         0.95,
		AuthFailurePenalty:     -10.0,
		ValidBlockBonus:        2.0,
		InvalidBlockPenalty:    -5.0,
		ValidTxBonus:           1.0,
		InvalidTxPenalty:       -2.0,
		UptimeBonus:            0.1,
		ResponseTimeBonus:      0.5,
		BandwidthPenalty:       -0.1,
		AutoAllowlistThreshold: 80.0,
		AutoBlacklistThreshold: 5.0,
		ScoreUpdateInterval:    1 * time.Minute,
		RateLimitConfig:        DefaultRateLimitConfig(),
	}
}

type PeerScoringManager struct {
	scores           map[peer.ID]*PeerScore
	config           *PeerScoringConfig
	network          *Libp2pNetwork
	mu               sync.RWMutex
	stopChan         chan struct{}
	rateLimitManager *RateLimitManager
}

func NewPeerScoringManager(network *Libp2pNetwork, config *PeerScoringConfig) *PeerScoringManager {
	if config == nil {
		config = DefaultPeerScoringConfig()
	}

	psm := &PeerScoringManager{
		scores:           make(map[peer.ID]*PeerScore),
		config:           config,
		network:          network,
		stopChan:         make(chan struct{}),
		rateLimitManager: NewRateLimitManager(config.RateLimitConfig),
	}

	go psm.scoreManagementLoop()

	return psm
}

func (psm *PeerScoringManager) GetPeerScore(peerID peer.ID) float64 {
	psm.mu.RLock()
	defer psm.mu.RUnlock()

	if score, exists := psm.scores[peerID]; exists {
		return score.Score
	}
	return 0.0
}

func (psm *PeerScoringManager) UpdatePeerScore(peerID peer.ID, eventType string, value interface{}) {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	score, exists := psm.scores[peerID]
	if !exists {
		score = &PeerScore{
			PeerID:      peerID,
			Score:       50.0,
			LastUpdated: time.Now(),
			LastSeen:    time.Now(),
		}
		psm.scores[peerID] = score
	}

	switch eventType {
	case "auth_success":
		score.Score += 5.0
		score.AuthFailures = 0 // Reset auth failures on success
	case "auth_failure":
		score.Score += psm.config.AuthFailurePenalty
		score.AuthFailures++
	case "valid_block":
		score.Score += psm.config.ValidBlockBonus
		score.ValidBlocks++
	case "invalid_block":
		score.Score += psm.config.InvalidBlockPenalty
		score.InvalidBlocks++
	case "valid_tx":
		score.Score += psm.config.ValidTxBonus
		score.ValidTxs++
	case "invalid_tx":
		score.Score += psm.config.InvalidTxPenalty
		score.InvalidTxs++
	case "connection":
		score.ConnectionCount++
		score.Score += 1.0
	case "disconnection":
		score.Score -= 1.0
	case "uptime":
		if uptime, ok := value.(time.Duration); ok {
			score.Uptime = uptime
			score.Score += psm.config.UptimeBonus * float64(uptime.Hours())
		}
	case "response_time":
		if responseTime, ok := value.(time.Duration); ok {
			score.ResponseTime = responseTime
			if responseTime < 100*time.Millisecond {
				score.Score += psm.config.ResponseTimeBonus
			}
		}
	case "bandwidth":
		if bandwidth, ok := value.(int64); ok {
			score.BandwidthUsage = bandwidth
			if bandwidth > 1024*1024 {
				score.Score += psm.config.BandwidthPenalty
			}
		}
	case "rate_limit_violation":
		if violation, ok := value.(map[string]interface{}); ok {
			action, _ := violation["action"].(string)
			switch action {
			case "auth":
				score.Score += -5.0
			case "stream":
				score.Score += -3.0
			case "bandwidth":
				score.Score += -2.0
			case "message":
				score.Score += -1.0
			case "block_sync":
				score.Score += -4.0
			case "transaction":
				score.Score += -2.0
			case "connection":
				score.Score += -3.0
			default:
				score.Score += -1.0
			}
		}
	}

	score.LastUpdated = time.Now()
	score.LastSeen = time.Now()

	if score.Score < 0 {
		score.Score = 0
	}
	if score.Score > 100 {
		score.Score = 100
	}

	logx.Info("PEER_SCORING", "Updated score for peer", peerID.String()[:12]+"...",
		"score:", score.Score, "event:", eventType)
}

func (psm *PeerScoringManager) CheckRateLimit(peerID peer.ID, actionType string, value interface{}) bool {
	if psm.rateLimitManager == nil {
		return true
	}

	limiter := psm.rateLimitManager.GetPeerRateLimiter(peerID)

	switch actionType {
	case "auth":
		return limiter.CheckAuthRateLimit()
	case "stream":
		return limiter.CheckStreamRateLimit()
	case "bandwidth":
		if bytes, ok := value.(int64); ok {
			return limiter.CheckBandwidthRateLimit(bytes)
		}
		return true
	case "block_sync":
		return limiter.CheckBlockSyncRateLimit()
	default:
		return true
	}
}

func (psm *PeerScoringManager) RecordRateLimitViolation(peerID peer.ID, actionType string, value interface{}) {
	psm.UpdatePeerScore(peerID, "rate_limit_violation", map[string]interface{}{
		"action": actionType,
		"value":  value,
	})

	logx.Warn("RATE_LIMIT", "Rate limit violation for peer", peerID.String()[:12]+"...",
		"action:", actionType, "value:", value)
}

func (psm *PeerScoringManager) GetRateLimitStatus(peerID peer.ID) map[string]interface{} {
	if psm.rateLimitManager == nil {
		return nil
	}

	limiter := psm.rateLimitManager.GetPeerRateLimiter(peerID)
	return limiter.GetRateLimitStatus()
}

func (psm *PeerScoringManager) GetGlobalRateLimitStatus() map[peer.ID]map[string]interface{} {
	if psm.rateLimitManager == nil {
		return nil
	}

	return psm.rateLimitManager.GetGlobalRateLimitStatus()
}

func (psm *PeerScoringManager) AutoManageAccessControl() {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	for peerID, score := range psm.scores {
		if score.Score >= psm.config.AutoAllowlistThreshold {
			if !psm.network.IsAllowed(peerID) {
				psm.network.AddToAllowlist(peerID)
				logx.Info("PEER_SCORING", "Auto-allowlisted peer "+peerID.String()[:12]+" (score: "+fmt.Sprintf("%.2f", score.Score)+")")
			}
		}

		if score.Score <= psm.config.AutoBlacklistThreshold {
			if psm.network.IsAllowed(peerID) {
				psm.network.AddToBlacklist(peerID)
				logx.Info("PEER_SCORING", "Auto-blacklisted peer "+peerID.String()[:12]+" (score: "+fmt.Sprintf("%.2f", score.Score)+")")
			}
		}
	}
}

func (psm *PeerScoringManager) scoreManagementLoop() {
	ticker := time.NewTicker(psm.config.ScoreUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-psm.stopChan:
			return
		case <-ticker.C:
			psm.decayScores()
			psm.AutoManageAccessControl()
			psm.cleanupOldScores()
		}
	}
}

func (psm *PeerScoringManager) decayScores() {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	for _, score := range psm.scores {
		score.Score *= psm.config.ScoreDecayRate

		if time.Since(score.LastSeen) > 24*time.Hour {
			score.Score *= 0.9
		}

		if score.Score < 0 {
			score.Score = 0
		}
	}
}

func (psm *PeerScoringManager) cleanupOldScores() {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for peerID, score := range psm.scores {
		if score.LastSeen.Before(cutoff) && score.Score < 10 {
			delete(psm.scores, peerID)
			logx.Info("PEER_SCORING", "Cleaned up old peer score: "+peerID.String()[:12])
		}
	}
}

func (psm *PeerScoringManager) GetTopPeers(n int) []*PeerScore {
	psm.mu.RLock()
	defer psm.mu.RUnlock()

	scores := make([]*PeerScore, 0, len(psm.scores))
	for _, score := range psm.scores {
		scores = append(scores, score)
	}

	for i := 0; i < len(scores)-1; i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[i].Score < scores[j].Score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	if n > len(scores) {
		n = len(scores)
	}
	return scores[:n]
}

func (psm *PeerScoringManager) GetPeerStats(peerID peer.ID) *PeerScore {
	psm.mu.RLock()
	defer psm.mu.RUnlock()

	if score, exists := psm.scores[peerID]; exists {
		return score
	}
	return nil
}

func (psm *PeerScoringManager) Stop() {
	close(psm.stopChan)
}
