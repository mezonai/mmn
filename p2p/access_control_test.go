package p2p

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
)

func TestInitializeAccessControl(t *testing.T) {
	network := &Libp2pNetwork{}

	// Initialize access control
	network.InitializeAccessControl()

	// Verify maps are initialized
	network.listMu.RLock()
	assert.NotNil(t, network.allowlist)
	assert.NotNil(t, network.blacklist)
	network.listMu.RUnlock()
}

func TestAllowlistOperations(t *testing.T) {
	network := &Libp2pNetwork{}
	network.InitializeAccessControl()

	peerID := peer.ID("test-peer")

	// Test adding to allowlist
	network.AddToAllowlist(peerID)

	network.listMu.RLock()
	assert.True(t, network.allowlist[peerID])
	network.listMu.RUnlock()

	// Test duplicate addition
	network.AddToAllowlist(peerID)

	network.listMu.RLock()
	assert.True(t, network.allowlist[peerID])
	network.listMu.RUnlock()
}

func TestBlacklistOperations(t *testing.T) {
	network := &Libp2pNetwork{}
	network.InitializeAccessControl()

	peerID := peer.ID("test-peer")

	// Test adding to blacklist
	network.AddToBlacklist(peerID)

	network.listMu.RLock()
	assert.True(t, network.blacklist[peerID])
	network.listMu.RUnlock()
}

func TestBlacklistWithExpiry(t *testing.T) {
	network := &Libp2pNetwork{}
	network.InitializeAccessControl()

	peerID := peer.ID("test-peer")

	// Test temporary blacklist
	network.AddToBlacklistWithExpiry(peerID, 100*time.Millisecond)

	network.listMu.RLock()
	assert.True(t, network.blacklist[peerID])
	network.listMu.RUnlock()

	// Wait for expiry
	time.Sleep(150 * time.Millisecond)

	network.listMu.RLock()
	_, exists := network.blacklist[peerID]
	network.listMu.RUnlock()
	assert.False(t, exists, "Peer should be removed from blacklist after expiry")
}

func TestEnableDisableLists(t *testing.T) {
	network := &Libp2pNetwork{}
	network.InitializeAccessControl()

	peerID := peer.ID("test-peer")

	// Test with both lists disabled (default behavior)
	assert.True(t, network.IsAllowed(peerID), "Peer should be allowed when lists are disabled")

	// Enable allowlist
	network.EnableAllowlist(true)
	assert.False(t, network.IsAllowed(peerID), "Peer should not be allowed when allowlist is enabled but empty")

	// Add peer to allowlist
	network.AddToAllowlist(peerID)
	assert.True(t, network.IsAllowed(peerID), "Peer should be allowed when in allowlist")

	// Enable blacklist
	network.EnableBlacklist(true)
	network.AddToBlacklist(peerID)
	assert.False(t, network.IsAllowed(peerID), "Blacklisted peer should not be allowed")

	// Remove from blacklist
	network.listMu.Lock()
	delete(network.blacklist, peerID)
	network.listMu.Unlock()
	assert.True(t, network.IsAllowed(peerID), "Peer should be allowed after removal from blacklist")
}

func TestIsBlacklisted(t *testing.T) {
	network := &Libp2pNetwork{}
	network.InitializeAccessControl()

	peerID := peer.ID("test-peer")

	// Test when blacklist is disabled
	assert.False(t, network.IsBlacklisted(peerID))

	// Enable blacklist and add peer
	network.EnableBlacklist(true)
	network.AddToBlacklist(peerID)

	assert.True(t, network.IsBlacklisted(peerID))
}

func TestAutoAddToAllowlistIfBootstrap(t *testing.T) {
	network := &Libp2pNetwork{}
	network.InitializeAccessControl()
	network.EnableAllowlist(true)

	peerID := peer.ID("test-peer")

	// Test auto-allowlist for bootstrap peer
	network.AutoAddToAllowlistIfBootstrap(peerID)

	network.listMu.RLock()
	assert.True(t, network.allowlist[peerID])
	network.listMu.RUnlock()
}

func TestAccessControlUpdateHandling(t *testing.T) {
	// Skip this test for now as it requires a mock host
	t.Skip("Skipping test that requires mock host implementation")
}

func TestAccessControlSyncHandling(t *testing.T) {
	// Skip this test for now as it requires a mock host
	t.Skip("Skipping test that requires mock host implementation")
}

func TestPeerScoringIntegration(t *testing.T) {
	network := &Libp2pNetwork{}
	network.InitializeAccessControl()

	// Test that UpdatePeerScore works
	peerID := peer.ID("test-peer")

	// This should not panic even without peer scoring manager
	network.UpdatePeerScore(peerID, "auth_success", nil)

	// Test getters
	score := network.GetPeerScore(peerID)
	assert.Equal(t, 0.0, score) // Default when no manager

	topPeers := network.GetTopPeers(5)
	assert.Nil(t, topPeers) // Nil when no manager

	stats := network.GetPeerStats(peerID)
	assert.Nil(t, stats) // Nil when no manager
}

func TestAccessControlEdgeCases(t *testing.T) {
	network := &Libp2pNetwork{}

	// Test with uninitialized lists
	peerID := peer.ID("test-peer")

	// Should not panic
	assert.True(t, network.IsAllowed(peerID))
	assert.False(t, network.IsBlacklisted(peerID))

	// Test adding to uninitialized lists
	network.AddToAllowlist(peerID)
	network.AddToBlacklist(peerID)

	// Lists should be auto-initialized
	network.listMu.RLock()
	assert.NotNil(t, network.allowlist)
	assert.NotNil(t, network.blacklist)
	network.listMu.RUnlock()
}
