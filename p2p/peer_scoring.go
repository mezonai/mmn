package p2p

import (
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mezonai/mmn/logx"
)

type PeerScore struct {
	PeerID                peer.ID
	Score                 float64
	LastUpdated           time.Time
	ConnectionCount       int
	AuthFailures          int
	AuthFailureTimestamps []time.Time
	ValidBlocks           int
	InvalidBlocks         int
	ValidTxs              int
	InvalidTxs            int
	Uptime                time.Duration
	ResponseTime          time.Duration
	BandwidthUsage        int64
	LastSeen              time.Time
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
}

func DefaultPeerScoringConfig() *PeerScoringConfig {
	return &PeerScoringConfig{
		MinScoreForAllowlist: 50.0,
		MaxScoreForBlacklist: -20.0,
		// ~24h half-life per minute tick ≈ 0.9995
		ScoreDecayRate:         0.9995,
		AuthFailurePenalty:     -20.0,
		ValidBlockBonus:        0.5,
		InvalidBlockPenalty:    -80.0,
		ValidTxBonus:           0.1,
		InvalidTxPenalty:       -3.0,
		UptimeBonus:            0.1,
		ResponseTimeBonus:      0.2,
		BandwidthPenalty:       -0.5,
		AutoAllowlistThreshold: 50.0,
		AutoBlacklistThreshold: -20.0,
		ScoreUpdateInterval:    1 * time.Minute,
	}
}

type PeerScoringManager struct {
	scores   map[peer.ID]*PeerScore
	config   *PeerScoringConfig
	network  *Libp2pNetwork
	mu       sync.RWMutex
	stopChan chan struct{}
}

func NewPeerScoringManager(network *Libp2pNetwork, config *PeerScoringConfig) *PeerScoringManager {
	if config == nil {
		config = DefaultPeerScoringConfig()
	}

	psm := &PeerScoringManager{
		scores:   make(map[peer.ID]*PeerScore),
		config:   config,
		network:  network,
		stopChan: make(chan struct{}),
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

// OnTopicMessage is a no-op placeholder to satisfy validator hooks.
// It can be extended later to compute time-in-mesh, first-delivery and spam counters.
func (psm *PeerScoringManager) OnTopicMessage(peerID peer.ID, topic string, data []byte) {
	// intentionally left blank for now
}

func (psm *PeerScoringManager) UpdatePeerScore(peerID peer.ID, eventType string, value interface{}) {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	score, exists := psm.scores[peerID]
	if !exists {
		score = &PeerScore{
			PeerID:      peerID,
			Score:       0.0,
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
		// avoid piling penalties if peer is currently blacklisted (connection will be rejected anyway)
		if psm.network == nil || !psm.network.IsBlacklisted(peerID) {
			score.Score += psm.config.AuthFailurePenalty
		}
		score.AuthFailures++
		score.AuthFailureTimestamps = append(score.AuthFailureTimestamps, time.Now())
		// Blacklist if 3 auth failures occur within 10 minutes
		cutoff := time.Now().Add(-10 * time.Minute)
		recent := 0
		for i := len(score.AuthFailureTimestamps) - 1; i >= 0; i-- {
			if score.AuthFailureTimestamps[i].After(cutoff) {
				recent++
				if recent >= 3 {
					psm.network.AddToBlacklist(peerID)
					break
				}
			} else {
				break
			}
		}
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
			if responseTime < 500*time.Millisecond {
				score.Score += psm.config.ResponseTimeBonus
			} else if responseTime > 2*time.Second {
				score.Score -= 0.5
			}
		}
	case "bandwidth":
		if bandwidth, ok := value.(int64); ok {
			score.BandwidthUsage = bandwidth
			if bandwidth > 5*1024*1024 {
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
	case "score_delta":
		if delta, ok := value.(float64); ok {
			score.Score += delta
		}
	}

	score.LastUpdated = time.Now()
	score.LastSeen = time.Now()

	peerIDStr := peerID.String()
	if len(peerIDStr) > 12 {
		peerIDStr = peerIDStr[:12] + "..."
	}
	logx.Info("PEER_SCORING", "Updated score for peer ", peerIDStr,
		"score: ", score.Score, " event: ", eventType)
}

func (psm *PeerScoringManager) AutoManageAccessControl() {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	for peerID, score := range psm.scores {
		if score.Score >= psm.config.AutoAllowlistThreshold {
			if !psm.network.IsAllowed(peerID) {
				psm.network.AddToAllowlist(peerID)
				peerIDStr := peerID.String()
				if len(peerIDStr) > 12 {
					peerIDStr = peerIDStr[:12]
				}
				logx.Info("PEER_SCORING", "Auto-allowlisted peer "+peerIDStr+" (score: "+fmt.Sprintf("%.2f", score.Score)+")")
			}
		}

		if score.Score <= psm.config.AutoBlacklistThreshold {
			psm.network.AddToBlacklist(peerID)
			peerIDStr := peerID.String()
			if len(peerIDStr) > 12 {
				peerIDStr = peerIDStr[:12]
			}
			logx.Info("PEER_SCORING", "Auto-blacklisted peer "+peerIDStr+" (score: "+fmt.Sprintf("%.2f", score.Score)+")")
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
	}
}

func (psm *PeerScoringManager) cleanupOldScores() {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for peerID, score := range psm.scores {
		if score.LastSeen.Before(cutoff) && score.Score < 10 {
			delete(psm.scores, peerID)
			peerIDStr := peerID.String()
			if len(peerIDStr) > 12 {
				peerIDStr = peerIDStr[:12]
			}
			logx.Info("PEER_SCORING", "Cleaned up old peer score: "+peerIDStr)
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
