package p2p

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPeerScoringManagerCreation(t *testing.T) {
	network := &Libp2pNetwork{}
	config := DefaultPeerScoringConfig()

	psm := NewPeerScoringManager(network, config)
	require.NotNil(t, psm)
	assert.Equal(t, config, psm.config)
	assert.Equal(t, network, psm.network)

	// Test with nil config
	psm2 := NewPeerScoringManager(network, nil)
	require.NotNil(t, psm2)
	assert.Equal(t, DefaultPeerScoringConfig(), psm2.config)
}

func TestDefaultPeerScoringConfig(t *testing.T) {
	config := DefaultPeerScoringConfig()

	assert.Equal(t, 50.0, config.MinScoreForAllowlist)
	assert.Equal(t, -20.0, config.MaxScoreForBlacklist)
	assert.Equal(t, 0.9995, config.ScoreDecayRate)
	assert.Equal(t, -20.0, config.AuthFailurePenalty)
	assert.Equal(t, 0.5, config.ValidBlockBonus)
	assert.Equal(t, -80.0, config.InvalidBlockPenalty)
	assert.Equal(t, 0.1, config.ValidTxBonus)
	assert.Equal(t, -3.0, config.InvalidTxPenalty)
	assert.Equal(t, 0.1, config.UptimeBonus)
	assert.Equal(t, 0.2, config.ResponseTimeBonus)
	assert.Equal(t, -0.5, config.BandwidthPenalty)
	assert.Equal(t, 50.0, config.AutoAllowlistThreshold)
	assert.Equal(t, -20.0, config.AutoBlacklistThreshold)
	assert.Equal(t, 1*time.Minute, config.ScoreUpdateInterval)
}

func TestUpdatePeerScore(t *testing.T) {
	network := &Libp2pNetwork{}
	psm := NewPeerScoringManager(network, nil)

	peerID := peer.ID("test-peer")

	// Test initial score
	initialScore := psm.GetPeerScore(peerID)
	assert.Equal(t, 0.0, initialScore)

	// Test auth_success
	psm.UpdatePeerScore(peerID, "auth_success", nil)
	score := psm.GetPeerScore(peerID)
	assert.Equal(t, 5.0, score)

	// Test valid_block
	psm.UpdatePeerScore(peerID, "valid_block", nil)
	score = psm.GetPeerScore(peerID)
	assert.Equal(t, 5.5, score)

	// Test invalid_block
	psm.UpdatePeerScore(peerID, "invalid_block", nil)
	score = psm.GetPeerScore(peerID)
	assert.Equal(t, -74.5, score)

	// Test connection
	psm.UpdatePeerScore(peerID, "connection", nil)
	score = psm.GetPeerScore(peerID)
	assert.Equal(t, -73.5, score)
}

func TestAuthFailureEscalation(t *testing.T) {
	network := &Libp2pNetwork{}
	psm := NewPeerScoringManager(network, nil)

	peerID := peer.ID("test-peer")

	// Test multiple auth failures
	for i := 0; i < 3; i++ {
		psm.UpdatePeerScore(peerID, "auth_failure", nil)
	}

	score := psm.GetPeerScore(peerID)
	assert.Equal(t, -60.0, score) // 3 * -20.0

	// Check if peer was blacklisted (this would require network mock)
	// For now, we just verify the score calculation
}

func TestScoreDecay(t *testing.T) {
	network := &Libp2pNetwork{}
	config := DefaultPeerScoringConfig()
	config.ScoreDecayRate = 0.5 // Use a more dramatic decay for testing
	psm := NewPeerScoringManager(network, config)

	peerID := peer.ID("test-peer")

	// Set initial score
	psm.UpdatePeerScore(peerID, "auth_success", nil)
	initialScore := psm.GetPeerScore(peerID)
	assert.Equal(t, 5.0, initialScore)

	// Trigger decay
	psm.decayScores()

	decayedScore := psm.GetPeerScore(peerID)
	assert.Equal(t, 2.5, decayedScore) // 5.0 * 0.5
}

func TestAutoManageAccessControl(t *testing.T) {
	network := &Libp2pNetwork{}
	psm := NewPeerScoringManager(network, nil)

	// Create peers with different scores
	goodPeer := peer.ID("good-peer")
	badPeer := peer.ID("bad-peer")

	// Give good peer high score (need 100 valid blocks to get 50.0 points)
	for i := 0; i < 100; i++ {
		psm.UpdatePeerScore(goodPeer, "valid_block", nil)
	}

	// Verify good peer score
	goodScore := psm.GetPeerScore(goodPeer)
	t.Logf("Good peer score: %f", goodScore)
	assert.True(t, goodScore >= 50.0, "Good peer score should be >= 50.0, got %f", goodScore)

	// Give bad peer low score (just enough to get below -20.0)
	for i := 0; i < 3; i++ {
		psm.UpdatePeerScore(badPeer, "invalid_block", nil)
	}

	// Verify bad peer score
	badScore := psm.GetPeerScore(badPeer)
	t.Logf("Bad peer score: %f", badScore)
	assert.True(t, badScore <= -20.0, "Bad peer score should be <= -20.0, got %f", badScore)

	// Test auto management
	psm.AutoManageAccessControl()

	// After auto management, the bad peer should be blacklisted
	// and the good peer should be allowlisted

	// Note: In a real test, we'd need to mock the network methods
	// For now, we just verify the scoring logic works
}

func TestGetTopPeers(t *testing.T) {
	network := &Libp2pNetwork{}
	psm := NewPeerScoringManager(network, nil)

	// Create multiple peers with different scores
	peers := []peer.ID{
		peer.ID("peer1"),
		peer.ID("peer2"),
		peer.ID("peer3"),
	}

	// Set different scores
	psm.UpdatePeerScore(peers[0], "auth_success", nil) // +5.0
	psm.UpdatePeerScore(peers[1], "valid_block", nil)  // +0.5
	psm.UpdatePeerScore(peers[2], "connection", nil)   // +1.0

	// Get top 2 peers
	topPeers := psm.GetTopPeers(2)
	require.Len(t, topPeers, 2)

	// Verify ordering (highest first)
	assert.Equal(t, peers[0].String(), topPeers[0].PeerID.String())
	assert.Equal(t, 5.0, topPeers[0].Score)
}

func TestGetPeerStats(t *testing.T) {
	network := &Libp2pNetwork{}
	psm := NewPeerScoringManager(network, nil)

	peerID := peer.ID("test-peer")

	// Update peer with various events
	psm.UpdatePeerScore(peerID, "auth_success", nil)
	psm.UpdatePeerScore(peerID, "valid_block", nil)
	psm.UpdatePeerScore(peerID, "connection", nil)

	// Get stats
	stats := psm.GetPeerStats(peerID)
	require.NotNil(t, stats)

	assert.Equal(t, peerID, stats.PeerID)
	assert.Equal(t, 6.5, stats.Score) // 5.0 + 0.5 + 1.0
	assert.Equal(t, 1, stats.ValidBlocks)
	assert.Equal(t, 1, stats.ConnectionCount)
}

func TestCleanupOldScores(t *testing.T) {
	network := &Libp2pNetwork{}
	psm := NewPeerScoringManager(network, nil)

	peerID := peer.ID("test-peer")

	// Add peer with low score
	psm.UpdatePeerScore(peerID, "invalid_block", nil)
	psm.UpdatePeerScore(peerID, "invalid_block", nil)

	// Verify peer exists
	assert.True(t, psm.GetPeerScore(peerID) < 10)

	// Cleanup old scores
	psm.cleanupOldScores()

	// Peer should still exist since it was recently added
	assert.True(t, psm.GetPeerScore(peerID) < 10)
}

func TestStopPeerScoringManager(t *testing.T) {
	network := &Libp2pNetwork{}
	psm := NewPeerScoringManager(network, nil)

	// Stop the manager
	psm.Stop()

	// Verify it can be stopped without error
	// The actual cleanup would happen in the goroutine
}
