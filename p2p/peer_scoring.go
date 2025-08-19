package p2p

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mezonai/mmn/logx"
)

// PeerScore represents the scoring system for a peer
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

// PeerScoringConfig defines thresholds for peer scoring
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

// DefaultPeerScoringConfig returns default scoring configuration
func DefaultPeerScoringConfig() *PeerScoringConfig {
	return &PeerScoringConfig{
		MinScoreForAllowlist:   50.0,  // Score cần để được allowlist
		MaxScoreForBlacklist:   10.0,  // Score thấp sẽ bị blacklist
		ScoreDecayRate:         0.95,  // Score giảm 5% mỗi interval
		AuthFailurePenalty:     -10.0, // Penalty cho auth failure
		ValidBlockBonus:        2.0,   // Bonus cho valid block
		InvalidBlockPenalty:    -5.0,  // Penalty cho invalid block
		ValidTxBonus:           1.0,   // Bonus cho valid transaction
		InvalidTxPenalty:       -2.0,  // Penalty cho invalid transaction
		UptimeBonus:            0.1,   // Bonus cho uptime (per hour)
		ResponseTimeBonus:      0.5,   // Bonus cho fast response
		BandwidthPenalty:       -0.1,  // Penalty cho high bandwidth usage
		AutoAllowlistThreshold: 80.0,  // Tự động add vào allowlist
		AutoBlacklistThreshold: 5.0,   // Tự động add vào blacklist
		ScoreUpdateInterval:    5 * time.Minute,
	}
}

// PeerScoringManager manages peer scoring and auto allowlist/blacklist
type PeerScoringManager struct {
	scores   map[peer.ID]*PeerScore
	config   *PeerScoringConfig
	network  *Libp2pNetwork
	mu       sync.RWMutex
	stopChan chan struct{}
}

// NewPeerScoringManager creates a new peer scoring manager
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

	// Start background score management
	go psm.scoreManagementLoop()

	return psm
}

// GetPeerScore returns the current score for a peer
func (psm *PeerScoringManager) GetPeerScore(peerID peer.ID) float64 {
	psm.mu.RLock()
	defer psm.mu.RUnlock()

	if score, exists := psm.scores[peerID]; exists {
		return score.Score
	}
	return 0.0
}

// UpdatePeerScore updates the score for a peer based on an event
func (psm *PeerScoringManager) UpdatePeerScore(peerID peer.ID, eventType string, value interface{}) {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	score, exists := psm.scores[peerID]
	if !exists {
		score = &PeerScore{
			PeerID:      peerID,
			Score:       50.0, // Default starting score
			LastUpdated: time.Now(),
			LastSeen:    time.Now(),
		}
		psm.scores[peerID] = score
	}

	// Update score based on event type
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
			if bandwidth > 1024*1024 { // > 1MB
				score.Score += psm.config.BandwidthPenalty
			}
		}
	}

	score.LastUpdated = time.Now()
	score.LastSeen = time.Now()

	// Ensure score stays within reasonable bounds
	if score.Score < 0 {
		score.Score = 0
	}
	if score.Score > 100 {
		score.Score = 100
	}

	logx.Info("PEER_SCORING", "Updated score for peer", peerID.String()[:12]+"...",
		"score:", score.Score, "event:", eventType)
}

// AutoManageAccessControl automatically manages allowlist/blacklist based on scores
func (psm *PeerScoringManager) AutoManageAccessControl() {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	for peerID, score := range psm.scores {
		// Auto-allowlist high-scoring peers
		if score.Score >= psm.config.AutoAllowlistThreshold {
			if !psm.network.IsAllowed(peerID) {
				psm.network.AddToAllowlist(peerID)
				logx.Info("PEER_SCORING", "Auto-allowlisted peer %s (score: %.2f)",
					peerID.String()[:12], score.Score)
			}
		}

		// Auto-blacklist low-scoring peers
		if score.Score <= psm.config.AutoBlacklistThreshold {
			if psm.network.IsAllowed(peerID) {
				psm.network.AddToBlacklist(peerID)
				logx.Info("PEER_SCORING", "Auto-blacklisted peer %s (score: %.2f)",
					peerID.String()[:12], score.Score)
			}
		}
	}
}

// scoreManagementLoop runs background score management
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

// decayScores applies score decay over time
func (psm *PeerScoringManager) decayScores() {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	for _, score := range psm.scores {
		// Apply decay rate
		score.Score *= psm.config.ScoreDecayRate

		// Additional decay for inactive peers
		if time.Since(score.LastSeen) > 24*time.Hour {
			score.Score *= 0.9 // Extra 10% decay for inactive peers
		}

		// Ensure minimum score
		if score.Score < 0 {
			score.Score = 0
		}
	}
}

// cleanupOldScores removes very old and low-scoring peers
func (psm *PeerScoringManager) cleanupOldScores() {
	psm.mu.Lock()
	defer psm.mu.Unlock()

	cutoff := time.Now().Add(-7 * 24 * time.Hour) // 1 week ago
	for peerID, score := range psm.scores {
		if score.LastSeen.Before(cutoff) && score.Score < 10 {
			delete(psm.scores, peerID)
			logx.Info("PEER_SCORING", "Cleaned up old peer score: %s", peerID.String()[:12])
		}
	}
}

// GetTopPeers returns the top N peers by score
func (psm *PeerScoringManager) GetTopPeers(n int) []*PeerScore {
	psm.mu.RLock()
	defer psm.mu.RUnlock()

	// Convert to slice for sorting
	scores := make([]*PeerScore, 0, len(psm.scores))
	for _, score := range psm.scores {
		scores = append(scores, score)
	}

	// Sort by score (descending)
	for i := 0; i < len(scores)-1; i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[i].Score < scores[j].Score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	// Return top N
	if n > len(scores) {
		n = len(scores)
	}
	return scores[:n]
}

// GetPeerStats returns detailed statistics for a peer
func (psm *PeerScoringManager) GetPeerStats(peerID peer.ID) *PeerScore {
	psm.mu.RLock()
	defer psm.mu.RUnlock()

	if score, exists := psm.scores[peerID]; exists {
		return score
	}
	return nil
}

// Stop stops the peer scoring manager
func (psm *PeerScoringManager) Stop() {
	close(psm.stopChan)
}
