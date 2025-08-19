package p2p

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mezonai/mmn/logx"
)

// handleAuthStream handles incoming authentication requests
func (ln *Libp2pNetwork) handleAuthStream(s network.Stream) {
	defer s.Close()

	remotePeer := s.Conn().RemotePeer()
	logx.Info("AUTH", "Handling authentication request from peer: ", remotePeer.String())

	// Enforce max message size (2048 bytes)
	limited := &io.LimitedReader{R: s, N: 2048}
	data, err := io.ReadAll(limited)
	if err != nil {
		logx.Error("AUTH", "Failed to read auth message: ", err.Error())
		return
	}

	// If reader hit the limit, reject (means payload ≥ 2048)
	if limited.N <= 0 {
		logx.Error("AUTH", "Auth message too large from ", remotePeer.String())
		return
	}

	var authMsg map[string]interface{}
	if err := json.Unmarshal(data, &authMsg); err != nil {
		logx.Error("AUTH", "Failed to unmarshal auth message: ", err.Error())
		return
	}

	msgType, ok := authMsg["type"].(string)
	if !ok {
		logx.Error("AUTH", "Invalid message type from ", remotePeer.String())
		return
	}

	switch msgType {
	case "challenge":
		ln.handleAuthChallenge(s, remotePeer, authMsg)
	case "response":
		ln.handleAuthResponse(s, remotePeer, authMsg)
	default:
		logx.Error("AUTH", "Unknown auth message type: ", msgType)
	}
}

// handleAuthChallenge handles incoming authentication challenges
func (ln *Libp2pNetwork) handleAuthChallenge(s network.Stream, remotePeer peer.ID, msg map[string]interface{}) {
	// Extract challenge data
	challengeData, ok := msg["challenge"].(map[string]interface{})
	if !ok {
		logx.Error("AUTH", "Invalid challenge format from ", remotePeer.String())
		return
	}

	challengeBytes, err := json.Marshal(challengeData)
	if err != nil {
		logx.Error("AUTH", "Failed to marshal challenge data: ", err.Error())
		return
	}

	var challenge AuthChallenge
	if err := json.Unmarshal(challengeBytes, &challenge); err != nil {
		logx.Error("AUTH", "Failed to unmarshal challenge: ", err.Error())
		return
	}

	// Verify protocol version
	if challenge.Version != AuthVersion {
		logx.Error("AUTH", "Unsupported auth version from ", remotePeer.String(), ": ", challenge.Version)
		return
	}

	// Verify chain ID (optional - can be configurable)
	if challenge.ChainID != DefaultChainID {
		logx.Warn("AUTH", "Different chain ID from ", remotePeer.String(), ": expected ", DefaultChainID, ", got ", challenge.ChainID)
		// Don't return error, just log warning for now
	}

	// Verify challenge timestamp (prevent replay attacks)
	now := time.Now().Unix()
	if abs(now-challenge.Timestamp) > AuthTimeout {
		logx.Error("AUTH", "Challenge timestamp expired from ", remotePeer.String())
		return
	}

	// Create data to sign: challenge + nonce + chain_id (Ethereum-style)
	dataToSign := append(challenge.Challenge, []byte(fmt.Sprintf("%d", challenge.Nonce))...)
	dataToSign = append(dataToSign, []byte(challenge.ChainID)...)

	// Sign the data with our private key
	signature := ed25519.Sign(ln.selfPrivKey, dataToSign)

	// Get the actual public key from the host
	hostPubKey := ln.host.Peerstore().PubKey(ln.host.ID())

	// Convert to bytes
	pubKeyBytes, err := hostPubKey.Raw()
	if err != nil {
		logx.Error("AUTH", "Failed to get public key bytes: ", err.Error())
		return
	}

	// Encode as base64 for transmission
	pubKeyStr := base64.StdEncoding.EncodeToString(pubKeyBytes)

	// Create response
	response := AuthResponse{
		Version:   AuthVersion,
		Challenge: challenge.Challenge,
		Signature: signature,
		PeerID:    ln.host.ID().String(),
		PublicKey: pubKeyStr,
		Timestamp: time.Now().Unix(),
		Nonce:     challenge.Nonce,
		ChainID:   challenge.ChainID,
	}

	// Send response
	responseMsg := map[string]interface{}{
		"type":     "response",
		"response": response,
	}

	responseData, err := json.Marshal(responseMsg)
	if err != nil {
		logx.Error("AUTH", "Failed to marshal response: ", err.Error())
		return
	}

	if _, err := s.Write(responseData); err != nil {
		logx.Error("AUTH", "Failed to send auth response: ", err.Error())
		return
	}

}

// handleAuthResponse handles incoming authentication responses
func (ln *Libp2pNetwork) handleAuthResponse(s network.Stream, remotePeer peer.ID, msg map[string]interface{}) {
	// Extract response data
	responseData, ok := msg["response"].(map[string]interface{})
	if !ok {
		logx.Error("AUTH", "Invalid response format from ", remotePeer.String())
		return
	}

	responseBytes, err := json.Marshal(responseData)
	if err != nil {
		logx.Error("AUTH", "Failed to marshal response data: ", err.Error())
		return
	}

	var response AuthResponse
	if err := json.Unmarshal(responseBytes, &response); err != nil {
		logx.Error("AUTH", "Failed to unmarshal response: ", err.Error())
		return
	}

	// Verify response timestamp
	now := time.Now().Unix()
	if abs(now-response.Timestamp) > AuthTimeout {
		logx.Error("AUTH", "Response timestamp expired from ", remotePeer.String())
		return
	}

	// Get the original challenge we sent
	ln.challengeMu.RLock()
	originalChallenge, exists := ln.pendingChallenges[remotePeer]
	ln.challengeMu.RUnlock()

	if !exists {
		logx.Error("AUTH", "No pending challenge found for ", remotePeer.String())
		return
	}

	// Verify the challenge matches
	if !bytesEqual(originalChallenge, response.Challenge) {
		logx.Error("AUTH", "Challenge mismatch from ", remotePeer.String())
		return
	}

	// Parse the public key - handle both string and base64 encoded formats
	var pubKeyBytes []byte
	if len(response.PublicKey) == ed25519.PublicKeySize*2 {
		// Assume it's hex encoded
		var err error
		pubKeyBytes, err = hex.DecodeString(response.PublicKey)
		if err != nil {
			logx.Error("AUTH", "Failed to decode hex public key from: ", err)
			return
		}
	} else if len(response.PublicKey) == ed25519.PublicKeySize {
		// Assume it's raw bytes
		pubKeyBytes = []byte(response.PublicKey)
	} else {
		// Try base64 decoding
		var err error
		pubKeyBytes, err = base64.StdEncoding.DecodeString(response.PublicKey)
		if err != nil {
			logx.Error("AUTH", "Failed to decode base64 public key from: ", err)
			return
		}
	}

	if len(pubKeyBytes) != ed25519.PublicKeySize {
		logx.Error("AUTH", "Invalid public key size from : ", len(pubKeyBytes))
		return
	}

	// Verify protocol version
	if response.Version != AuthVersion {
		logx.Error("AUTH", "Unsupported auth version from ", remotePeer.String(), ": ", response.Version)
		return
	}

	// Verify chain ID
	if response.ChainID != DefaultChainID {
		logx.Warn("AUTH", "Different chain ID from ", remotePeer.String(), ": expected ", DefaultChainID, ", got ", response.ChainID)
	}

	// Create data that was signed: challenge + nonce + chain_id (Ethereum-style)
	dataToVerify := append(response.Challenge, []byte(fmt.Sprintf("%d", response.Nonce))...)
	dataToVerify = append(dataToVerify, []byte(response.ChainID)...)

	// Verify the signature
	if !ed25519.Verify(pubKeyBytes, dataToVerify, response.Signature) {
		logx.Error("AUTH", "Invalid signature from ", remotePeer.String())
		// Update peer score for auth failure
		ln.UpdatePeerScore(remotePeer, "auth_failure", nil)
		return
	}

	// Authentication successful
	ln.authMu.Lock()
	ln.authenticatedPeers[remotePeer] = &AuthenticatedPeer{
		PeerID:        remotePeer,
		PublicKey:     pubKeyBytes,
		AuthTimestamp: time.Now(),
		IsValid:       true,
	}
	ln.authMu.Unlock()

	// Clean up pending challenge
	ln.challengeMu.Lock()
	delete(ln.pendingChallenges, remotePeer)
	ln.challengeMu.Unlock()

	// Update peer info
	ln.mu.Lock()
	if peerInfo, exists := ln.peers[remotePeer]; exists {
		peerInfo.IsAuthenticated = true
		peerInfo.AuthTimestamp = time.Now()
	}
	ln.mu.Unlock()

	logx.Info("AUTH", "Authentication successful with ", remotePeer.String())

	// Update peer score for successful authentication
	ln.UpdatePeerScore(remotePeer, "auth_success", nil)
}

// InitiateAuthentication starts the authentication process with a peer
func (ln *Libp2pNetwork) InitiateAuthentication(ctx context.Context, peerID peer.ID) error {
	logx.Info("AUTH", "Initiating authentication with peer: ", peerID.String())

	// Create a random challenge
	challenge := make([]byte, AuthChallengeSize)
	if _, err := rand.Read(challenge); err != nil {
		return fmt.Errorf("failed to generate challenge: %w", err)
	}

	// Generate a random nonce (Ethereum-style)
	nonceBytes := make([]byte, 8)
	if _, err := rand.Read(nonceBytes); err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}
	nonce := uint64(nonceBytes[0])<<56 | uint64(nonceBytes[1])<<48 | uint64(nonceBytes[2])<<40 | uint64(nonceBytes[3])<<32 |
		uint64(nonceBytes[4])<<24 | uint64(nonceBytes[5])<<16 | uint64(nonceBytes[6])<<8 | uint64(nonceBytes[7])

	// Store the challenge and nonce
	ln.challengeMu.Lock()
	ln.pendingChallenges[peerID] = challenge
	ln.challengeMu.Unlock()

	// Get the actual public key from the host
	hostPubKey := ln.host.Peerstore().PubKey(ln.host.ID())

	// Convert to bytes
	pubKeyBytes, err := hostPubKey.Raw()
	if err != nil {
		return fmt.Errorf("failed to get public key bytes: %w", err)
	}

	// Encode as base64 for transmission
	pubKeyStr := base64.StdEncoding.EncodeToString(pubKeyBytes)

	logx.Info("AUTH", "Host public key length: ", len(pubKeyBytes), ", encoded: ", pubKeyStr)

	// Create challenge message (Ethereum-style)
	authChallenge := AuthChallenge{
		Version:   AuthVersion,
		Challenge: challenge,
		PeerID:    ln.host.ID().String(),
		PublicKey: pubKeyStr,
		Timestamp: time.Now().Unix(),
		Nonce:     nonce,
		ChainID:   DefaultChainID,
	}

	// Send challenge
	challengeMsg := map[string]interface{}{
		"type":      "challenge",
		"challenge": authChallenge,
	}

	challengeData, err := json.Marshal(challengeMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal challenge: %w", err)
	}

	// Open stream and send challenge
	stream, err := ln.host.NewStream(ctx, peerID, AuthProtocol)
	if err != nil {
		return fmt.Errorf("failed to open auth stream: %w", err)
	}
	defer stream.Close()

	if _, err := stream.Write(challengeData); err != nil {
		return fmt.Errorf("failed to send challenge: %w", err)
	}

	// Wait for response
	buf := make([]byte, 2048)
	n, err := stream.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var responseMsg map[string]interface{}
	if err := json.Unmarshal(buf[:n], &responseMsg); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Check message type
	msgType, ok := responseMsg["type"].(string)
	if !ok {
		return fmt.Errorf("invalid message type")
	}

	switch msgType {
	case "response":
		// Handle the response message
		responseData, ok := responseMsg["response"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid response format")
		}

		responseBytes, err := json.Marshal(responseData)
		if err != nil {
			return fmt.Errorf("failed to marshal response data: %w", err)
		}

		var response AuthResponse
		if err := json.Unmarshal(responseBytes, &response); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}

		// Verify response timestamp
		now := time.Now().Unix()
		if abs(now-response.Timestamp) > AuthTimeout {
			return fmt.Errorf("response timestamp expired")
		}

		// Get the original challenge we sent
		ln.challengeMu.RLock()
		originalChallenge, exists := ln.pendingChallenges[peerID]
		ln.challengeMu.RUnlock()

		if !exists {
			return fmt.Errorf("no pending challenge found")
		}

		// Verify the challenge matches
		if !bytesEqual(originalChallenge, response.Challenge) {
			return fmt.Errorf("challenge mismatch")
		}

		// Parse the public key - handle both string and base64 encoded formats
		var pubKeyBytes []byte
		if len(response.PublicKey) == ed25519.PublicKeySize*2 {
			// Assume it's hex encoded
			var err error
			pubKeyBytes, err = hex.DecodeString(response.PublicKey)
			if err != nil {
				return fmt.Errorf("failed to decode hex public key: %w", err)
			}
		} else if len(response.PublicKey) == ed25519.PublicKeySize {
			// Assume it's raw bytes
			pubKeyBytes = []byte(response.PublicKey)
		} else {
			// Try base64 decoding
			var err error
			pubKeyBytes, err = base64.StdEncoding.DecodeString(response.PublicKey)
			if err != nil {
				return fmt.Errorf("failed to decode base64 public key: %w", err)
			}
		}

		if len(pubKeyBytes) != ed25519.PublicKeySize {
			return fmt.Errorf("invalid public key size: got %d, expected %d", len(pubKeyBytes), ed25519.PublicKeySize)
		}

		// Verify protocol version
		if response.Version != AuthVersion {
			return fmt.Errorf("unsupported auth version: %s", response.Version)
		}

		// Verify chain ID
		if response.ChainID != DefaultChainID {
			logx.Warn("AUTH", "Different chain ID from peer ", peerID.String(), ": expected ", DefaultChainID, ", got ", response.ChainID)
		}

		// Create data that was signed: challenge + nonce + chain_id (Ethereum-style)
		dataToVerify := append(response.Challenge, []byte(fmt.Sprintf("%d", response.Nonce))...)
		dataToVerify = append(dataToVerify, []byte(response.ChainID)...)

		// Verify the signature
		if !ed25519.Verify(pubKeyBytes, dataToVerify, response.Signature) {
			return fmt.Errorf("invalid signature")
		}

		// Authentication successful
		ln.authMu.Lock()
		ln.authenticatedPeers[peerID] = &AuthenticatedPeer{
			PeerID:        peerID,
			PublicKey:     pubKeyBytes,
			AuthTimestamp: time.Now(),
			IsValid:       true,
		}
		ln.authMu.Unlock()

		// Clean up pending challenge
		ln.challengeMu.Lock()
		delete(ln.pendingChallenges, peerID)
		ln.challengeMu.Unlock()

		// Update peer info
		ln.mu.Lock()
		if peerInfo, exists := ln.peers[peerID]; exists {
			peerInfo.IsAuthenticated = true
			peerInfo.AuthTimestamp = time.Now()
		}
		ln.mu.Unlock()

		logx.Info("AUTH", "Authentication successful with ", peerID.String())
		return nil

	case "result":
		// Handle result message (for backward compatibility)
		resultData, ok := responseMsg["result"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid result format")
		}

		resultBytes, err := json.Marshal(resultData)
		if err != nil {
			return fmt.Errorf("failed to marshal result data: %w", err)
		}

		var result AuthResult
		if err := json.Unmarshal(resultBytes, &result); err != nil {
			return fmt.Errorf("failed to unmarshal result: %w", err)
		}

		if result.Success {
			logx.Info("AUTH", "Authentication successful with ", peerID.String())
			return nil
		} else {
			return fmt.Errorf("authentication failed: %s", result.Message)
		}

	default:
		return fmt.Errorf("unexpected response type: %s", msgType)
	}
}

// IsPeerAuthenticated checks if a peer has been authenticated
func (ln *Libp2pNetwork) IsPeerAuthenticated(peerID peer.ID) bool {
	ln.authMu.RLock()
	defer ln.authMu.RUnlock()

	if authPeer, exists := ln.authenticatedPeers[peerID]; exists {
		return authPeer.IsValid
	}
	return false
}

// GetAuthenticatedPeers returns all authenticated peers
func (ln *Libp2pNetwork) GetAuthenticatedPeers() []peer.ID {
	ln.authMu.RLock()
	defer ln.authMu.RUnlock()

	var peers []peer.ID
	for peerID, authPeer := range ln.authenticatedPeers {
		if authPeer.IsValid {
			peers = append(peers, peerID)
		}
	}
	return peers
}

// CleanupExpiredAuthentications removes expired authentication records
func (ln *Libp2pNetwork) CleanupExpiredAuthentications() {
	ln.authMu.Lock()
	defer ln.authMu.Unlock()

	now := time.Now()
	expirationTime := now.Add(-time.Duration(AuthTimeout) * time.Second)

	for peerID, authPeer := range ln.authenticatedPeers {
		if authPeer.AuthTimestamp.Before(expirationTime) {
			delete(ln.authenticatedPeers, peerID)
			logx.Info("AUTH", "Removed expired authentication for peer: ", peerID.String())
		}
	}
}

// Helper functions
func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
