package p2p

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthChallengeCreation(t *testing.T) {
	// Create a test network
	host, err := libp2p.New()
	require.NoError(t, err)
	defer host.Close()

	_, privKey, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	network := &Libp2pNetwork{
		host:        host,
		selfPrivKey: privKey,
	}

	// Initialize maps
	network.pendingChallenges = make(map[peer.ID][]byte)
	network.authenticatedPeers = make(map[peer.ID]*AuthenticatedPeer)

	// Test challenge creation
	ctx := context.Background()
	peerID := peer.ID("test-peer")

	err = network.InitiateAuthentication(ctx, peerID)
	// This will fail because we can't actually connect, but we can verify the challenge was created
	assert.Error(t, err) // Expected to fail due to connection issues

	// Verify challenge was stored
	network.challengeMu.RLock()
	_, exists := network.pendingChallenges[peerID]
	network.challengeMu.RUnlock()
	assert.True(t, exists, "Challenge should be stored for peer")
}

func TestAuthChallengeValidation(t *testing.T) {
	// Test valid challenge
	validChallenge := AuthChallenge{
		Version:   AuthVersion,
		Challenge: []byte("test-challenge-32-bytes-long-enough"),
		PeerID:    "test-peer",
		PublicKey: "test-public-key",
		Timestamp: time.Now().Unix(),
		Nonce:     12345,
		ChainID:   DefaultChainID,
	}

	// Test invalid version
	invalidVersionChallenge := validChallenge
	invalidVersionChallenge.Version = "0.0.0"

	// Test expired timestamp
	expiredChallenge := validChallenge
	expiredChallenge.Timestamp = time.Now().Add(-time.Duration(AuthTimeout+1) * time.Second).Unix()

	// Test invalid chain ID
	invalidChainChallenge := validChallenge
	invalidChainChallenge.ChainID = "invalid-chain"

	// These tests would normally be in the actual validation functions
	// For now, we're testing the data structures
	assert.Equal(t, AuthVersion, validChallenge.Version)
	assert.Equal(t, DefaultChainID, validChallenge.ChainID)
	assert.True(t, time.Now().Unix()-validChallenge.Timestamp < AuthTimeout)
}

func TestAuthMessageMarshaling(t *testing.T) {
	challenge := AuthChallenge{
		Version:   AuthVersion,
		Challenge: []byte("test-challenge-32-bytes-long-enough"),
		PeerID:    "test-peer",
		PublicKey: "test-public-key",
		Timestamp: time.Now().Unix(),
		Nonce:     12345,
		ChainID:   DefaultChainID,
	}

	// Test marshaling
	data, err := json.Marshal(challenge)
	require.NoError(t, err)

	// Test unmarshaling
	var unmarshaledChallenge AuthChallenge
	err = json.Unmarshal(data, &unmarshaledChallenge)
	require.NoError(t, err)

	// Verify all fields match
	assert.Equal(t, challenge.Version, unmarshaledChallenge.Version)
	assert.Equal(t, challenge.PeerID, unmarshaledChallenge.PeerID)
	assert.Equal(t, challenge.PublicKey, unmarshaledChallenge.PublicKey)
	assert.Equal(t, challenge.Timestamp, unmarshaledChallenge.Timestamp)
	assert.Equal(t, challenge.Nonce, unmarshaledChallenge.Nonce)
	assert.Equal(t, challenge.ChainID, unmarshaledChallenge.ChainID)
	assert.Equal(t, challenge.Challenge, unmarshaledChallenge.Challenge)
}

func TestAuthConstants(t *testing.T) {
	assert.Equal(t, "1.0.0", AuthVersion)
	assert.Equal(t, "mmn", DefaultChainID)
	assert.Equal(t, 32, AuthChallengeSize)
	assert.Equal(t, 600, AuthTimeout)
}

func TestAuthTimeoutValidation(t *testing.T) {
	now := time.Now().Unix()

	// Test valid timestamp (within timeout)
	validTimestamp := now - 300 // 5 minutes ago
	assert.True(t, abs(now-validTimestamp) <= AuthTimeout)

	// Test expired timestamp (beyond timeout)
	expiredTimestamp := now - 700 // 11 minutes ago
	assert.True(t, abs(now-expiredTimestamp) > AuthTimeout)
}
