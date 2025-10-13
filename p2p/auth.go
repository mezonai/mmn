package p2p

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mezonai/mmn/logx"
)

func (ln *Libp2pNetwork) handleAuthStream(s network.Stream) {
	defer s.Close()

	remotePeer := s.Conn().RemotePeer()
	logx.Info("AUTH", "Handling authentication request from peer: ", remotePeer.String())

	if ln.peerScoringManager != nil {
		if !ln.peerScoringManager.CheckRateLimit(remotePeer, "auth", nil) {
			ln.peerScoringManager.RecordRateLimitViolation(remotePeer, "auth", nil)
			return
		}
	}

	limited := &io.LimitedReader{R: s, N: AuthLimitMessagePayload}
	data, err := io.ReadAll(limited)
	if err != nil {
		logx.Error("AUTH", "Failed to read auth message: ", err.Error())
		return
	}

	if limited.N <= 0 {
		if ln.peerScoringManager != nil {
			ln.peerScoringManager.RecordRateLimitViolation(remotePeer, "bandwidth", AuthLimitMessagePayload)
		}
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

func (ln *Libp2pNetwork) handleAuthChallenge(s network.Stream, remotePeer peer.ID, msg map[string]interface{}) {
	challengeData, ok := msg["challenge"].(map[string]interface{})
	if !ok {
		logx.Error("AUTH", "Invalid challenge format from ", remotePeer.String())
		return
	}

	var nonce uint64
	if nonceInterface, exists := challengeData["nonce"]; exists {
		switch v := nonceInterface.(type) {
		case string:
			var err error
			nonce, err = strconv.ParseUint(v, 10, 64)
			if err != nil {
				logx.Error("AUTH", "Failed to parse nonce from string: ", err.Error())
				return
			}
		case float64:
			nonce = uint64(v)
		case int64:
			nonce = uint64(v)
		case uint64:
			nonce = v
		case int:
			nonce = uint64(v)
		case uint:
			nonce = uint64(v)
		default:
			logx.Error("AUTH", "Invalid nonce type from ", remotePeer.String(), ": ", fmt.Sprintf("%T", v))
			return
		}
	} else {
		logx.Error("AUTH", "Missing nonce in challenge from ", remotePeer.String())
		return
	}

	version, _ := challengeData["version"].(string)
	chainID, _ := challengeData["chain_id"].(string)
	peerID, _ := challengeData["peer_id"].(string)
	publicKey, _ := challengeData["public_key"].(string)

	var timestamp int64
	if tsInterface, exists := challengeData["timestamp"]; exists {
		switch v := tsInterface.(type) {
		case float64:
			timestamp = int64(v)
		case int64:
			timestamp = v
		case int:
			timestamp = int64(v)
		default:
			logx.Error("AUTH", "Invalid timestamp type from ", remotePeer.String())
			return
		}
	}

	var challengeBytes []byte
	if challengeInterface, exists := challengeData["challenge"]; exists {
		switch v := challengeInterface.(type) {
		case string:
			var err error
			challengeBytes, err = base64.StdEncoding.DecodeString(v)
			if err != nil {
				logx.Error("AUTH", "Failed to decode challenge from base64: ", err.Error())
				return
			}
		case []byte:
			challengeBytes = v
		default:
			logx.Error("AUTH", "Invalid challenge type from ", remotePeer.String())
			return
		}
	} else {
		logx.Error("AUTH", "Missing challenge from ", remotePeer.String())
		return
	}

	challenge := AuthChallenge{
		Version:   version,
		Challenge: challengeBytes,
		PeerID:    peerID,
		PublicKey: publicKey,
		Timestamp: timestamp,
		Nonce:     nonce,
		ChainID:   chainID,
	}

	if challenge.Version != AuthVersion {
		logx.Error("AUTH", "Unsupported auth version from ", remotePeer.String(), ": ", challenge.Version)
		return
	}

	if challenge.ChainID != DefaultChainID {
		logx.Warn("AUTH", "Different chain ID from ", remotePeer.String(), ": expected ", DefaultChainID, ", got ", challenge.ChainID)
	}

	now := time.Now().Unix()
	if abs(now-challenge.Timestamp) > AuthTimeout {
		logx.Error("AUTH", "Challenge timestamp expired from ", remotePeer.String())
		return
	}

	dataToSign := append(challenge.Challenge, []byte(fmt.Sprintf("%d", challenge.Nonce))...)
	dataToSign = append(dataToSign, []byte(challenge.ChainID)...)

	signature := ed25519.Sign(ln.selfPrivKey, dataToSign)

	hostPubKey := ln.host.Peerstore().PubKey(ln.host.ID())

	pubKeyBytes, err := hostPubKey.Raw()
	if err != nil {
		logx.Error("AUTH", "Failed to get public key bytes: ", err.Error())
		return
	}

	pubKeyStr := base64.StdEncoding.EncodeToString(pubKeyBytes)

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

func (ln *Libp2pNetwork) handleAuthResponse(s network.Stream, remotePeer peer.ID, msg map[string]interface{}) {
	responseData, ok := msg["response"].(map[string]interface{})
	if !ok {
		logx.Error("AUTH", "Invalid response format from ", remotePeer.String())
		return
	}

	var nonce uint64
	if nonceInterface, exists := responseData["nonce"]; exists {
		switch v := nonceInterface.(type) {
		case string:
			var err error
			nonce, err = strconv.ParseUint(v, 10, 64)
			if err != nil {
				logx.Error("AUTH", "Failed to parse nonce from string: ", err.Error())
				return
			}
		case float64:
			nonce = uint64(v)
		case int64:
			nonce = uint64(v)
		case uint64:
			nonce = v
		case int:
			nonce = uint64(v)
		case uint:
			nonce = uint64(v)
		default:
			logx.Error("AUTH", "Invalid nonce type in response from ", remotePeer.String(), ": ", fmt.Sprintf("%T", v))
			return
		}
	} else {
		logx.Error("AUTH", "Missing nonce in response from ", remotePeer.String())
		return
	}

	version, _ := responseData["version"].(string)
	chainID, _ := responseData["chain_id"].(string)
	peerID, _ := responseData["peer_id"].(string)
	publicKey, _ := responseData["public_key"].(string)

	var timestamp int64
	if tsInterface, exists := responseData["timestamp"]; exists {
		switch v := tsInterface.(type) {
		case float64:
			timestamp = int64(v)
		case int64:
			timestamp = v
		case int:
			timestamp = int64(v)
		default:
			logx.Error("AUTH", "Invalid timestamp type in response from ", remotePeer.String())
			return
		}
	}

	var challengeBytes []byte
	if challengeInterface, exists := responseData["challenge"]; exists {
		switch v := challengeInterface.(type) {
		case string:
			var err error
			challengeBytes, err = base64.StdEncoding.DecodeString(v)
			if err != nil {
				logx.Error("AUTH", "Failed to decode challenge from base64 in response: ", err.Error())
				return
			}
		case []byte:
			challengeBytes = v
		default:
			logx.Error("AUTH", "Invalid challenge type in response from ", remotePeer.String())
			return
		}
	} else {
		logx.Error("AUTH", "Missing challenge in response from ", remotePeer.String())
		return
	}

	var signature []byte
	if sigInterface, exists := responseData["signature"]; exists {
		switch v := sigInterface.(type) {
		case string:
			var err error
			signature, err = base64.StdEncoding.DecodeString(v)
			if err != nil {
				logx.Error("AUTH", "Failed to decode signature from base64: ", err.Error())
				return
			}
		case []byte:
			signature = v
		default:
			logx.Error("AUTH", "Invalid signature type from ", remotePeer.String())
			return
		}
	} else {
		logx.Error("AUTH", "Missing signature in response from ", remotePeer.String())
		return
	}

	response := AuthResponse{
		Version:   version,
		Challenge: challengeBytes,
		Signature: signature,
		PeerID:    peerID,
		PublicKey: publicKey,
		Timestamp: timestamp,
		Nonce:     nonce,
		ChainID:   chainID,
	}

	now := time.Now().Unix()
	if abs(now-response.Timestamp) > AuthTimeout {
		logx.Error("AUTH", "Response timestamp expired from ", remotePeer.String())
		return
	}

	ln.challengeMu.RLock()
	originalChallenge, exists := ln.pendingChallenges[remotePeer]
	ln.challengeMu.RUnlock()

	if !exists {
		logx.Error("AUTH", "No pending challenge found for ", remotePeer.String())
		return
	}

	if !bytesEqual(originalChallenge, response.Challenge) {
		logx.Error("AUTH", "Challenge mismatch from ", remotePeer.String())
		return
	}

	var pubKeyBytes []byte
	if len(response.PublicKey) == ed25519.PublicKeySize*2 {
		var err error
		pubKeyBytes, err = hex.DecodeString(response.PublicKey)
		if err != nil {
			logx.Error("AUTH", "Failed to decode hex public key from: ", err)
			return
		}
	} else if len(response.PublicKey) == ed25519.PublicKeySize {
		pubKeyBytes = []byte(response.PublicKey)
	} else {
		var err error
		pubKeyBytes, err = base64.StdEncoding.DecodeString(response.PublicKey)
		if err != nil {
			logx.Error("AUTH", "Failed to decode base64 public key from: ", err)
			return
		}
	}

	if len(pubKeyBytes) != ed25519.PublicKeySize {
		logx.Error("AUTH", "Invalid public key size from: ", len(pubKeyBytes))
		return
	}

	if response.Version != AuthVersion {
		logx.Error("AUTH", "Unsupported auth version from ", remotePeer.String(), " : ", response.Version)
		return
	}

	if response.ChainID != DefaultChainID {
		logx.Warn("AUTH", "Different chain ID from ", remotePeer.String(), ": expected ", DefaultChainID, ", got ", response.ChainID)
	}

	dataToVerify := append(response.Challenge, []byte(fmt.Sprintf("%d", response.Nonce))...)
	dataToVerify = append(dataToVerify, []byte(response.ChainID)...)

	if !ed25519.Verify(pubKeyBytes, dataToVerify, response.Signature) {
		ln.UpdatePeerScore(remotePeer, "auth_failure", nil)
		return
	}

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
	ln.listMu.Lock()
	if peerInfo, exists := ln.peers[remotePeer]; exists {
		peerInfo.IsAuthenticated = true
		peerInfo.AuthTimestamp = time.Now()
	}
	ln.listMu.Unlock()

	ln.UpdatePeerScore(remotePeer, "auth_success", nil)

	ln.AutoAddToAllowlistIfBootstrap(remotePeer)
}

func (ln *Libp2pNetwork) InitiateAuthentication(ctx context.Context, peerID peer.ID) error {
	// Avoid duplicate attempts: skip if already authenticated
	if ln.IsPeerAuthenticated(peerID) {
		return nil
	}
	// Skip if we already have a pending challenge to this peer
	ln.challengeMu.RLock()
	_, pending := ln.pendingChallenges[peerID]
	ln.challengeMu.RUnlock()
	if pending {
		return nil
	}

	logx.Info("AUTH", "Initiating authentication with peer: ", peerID.String())

	// Create a random challenge
	challenge := make([]byte, AuthChallengeSize)
	if _, err := rand.Read(challenge); err != nil {
		return fmt.Errorf("failed to generate challenge: %w", err)
	}

	nonceBytes := make([]byte, 8)
	if _, err := rand.Read(nonceBytes); err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}

	nonce := binary.BigEndian.Uint64(nonceBytes)

	ln.challengeMu.Lock()
	ln.pendingChallenges[peerID] = challenge
	ln.challengeMu.Unlock()

	hostPubKey := ln.host.Peerstore().PubKey(ln.host.ID())

	pubKeyBytes, err := hostPubKey.Raw()
	if err != nil {
		return fmt.Errorf("failed to get public key bytes: %w", err)
	}

	pubKeyStr := base64.StdEncoding.EncodeToString(pubKeyBytes)

	// Reduce verbosity to avoid log spam
	logx.Debug("AUTH", "Host public key length:", len(pubKeyBytes))

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

	if ln.peerScoringManager != nil {
		if !ln.peerScoringManager.CheckRateLimit(peerID, "stream", nil) {
			ln.peerScoringManager.RecordRateLimitViolation(peerID, "stream", nil)
			return fmt.Errorf("rate limited: stream")
		}
	}
	stream, err := ln.host.NewStream(ctx, peerID, AuthProtocol)
	if err != nil {
		return fmt.Errorf("failed to open auth stream: %w", err)
	}
	defer stream.Close()

	if ln.peerScoringManager != nil {
		if !ln.peerScoringManager.CheckRateLimit(peerID, "bandwidth", int64(len(challengeData))) {
			ln.peerScoringManager.RecordRateLimitViolation(peerID, "bandwidth", int64(len(challengeData)))
			return fmt.Errorf("rate limited: bandwidth")
		}
	}
	if _, err := stream.Write(challengeData); err != nil {
		return fmt.Errorf("failed to send challenge: %w", err)
	}

	buf := make([]byte, AuthLimitMessagePayload)
	n, err := stream.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var responseMsg map[string]interface{}
	if err := json.Unmarshal(buf[:n], &responseMsg); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	msgType, ok := responseMsg["type"].(string)
	if !ok {
		return fmt.Errorf("invalid message type")
	}

	switch msgType {
	case "response":
		responseData, ok := responseMsg["response"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid response format")
		}

		var nonce uint64
		if nonceInterface, exists := responseData["nonce"]; exists {
			switch v := nonceInterface.(type) {
			case string:
				nonce, err = strconv.ParseUint(v, 10, 64)
				if err != nil {
					return fmt.Errorf("failed to parse nonce from string: %w", err)
				}
			case float64:
				nonce = uint64(v)
			case int64:
				nonce = uint64(v)
			case uint64:
				nonce = v
			case int:
				nonce = uint64(v)
			case uint:
				nonce = uint64(v)
			default:
				return fmt.Errorf("invalid nonce type: %T", v)
			}
		} else {
			return fmt.Errorf("missing nonce in response")
		}

		version, _ := responseData["version"].(string)
		chainID, _ := responseData["chain_id"].(string)
		responsePeerID, _ := responseData["peer_id"].(string)
		publicKey, _ := responseData["public_key"].(string)

		var timestamp int64
		if tsInterface, exists := responseData["timestamp"]; exists {
			switch v := tsInterface.(type) {
			case float64:
				timestamp = int64(v)
			case int64:
				timestamp = v
			case int:
				timestamp = int64(v)
			default:
				return fmt.Errorf("invalid timestamp type: %T", v)
			}
		}

		var challengeBytes []byte
		if challengeInterface, exists := responseData["challenge"]; exists {
			switch v := challengeInterface.(type) {
			case string:
				challengeBytes, err = base64.StdEncoding.DecodeString(v)
				if err != nil {
					return fmt.Errorf("failed to decode challenge from base64: %w", err)
				}
			case []byte:
				challengeBytes = v
			default:
				return fmt.Errorf("invalid challenge type: %T", v)
			}
		} else {
			return fmt.Errorf("missing challenge in response")
		}

		var signature []byte
		if sigInterface, exists := responseData["signature"]; exists {
			switch v := sigInterface.(type) {
			case string:
				signature, err = base64.StdEncoding.DecodeString(v)
				if err != nil {
					return fmt.Errorf("failed to decode signature from base64: %w", err)
				}
			case []byte:
				signature = v
			default:
				return fmt.Errorf("invalid signature type: %T", v)
			}
		} else {
			return fmt.Errorf("missing signature in response")
		}

		response := AuthResponse{
			Version:   version,
			Challenge: challengeBytes,
			Signature: signature,
			PeerID:    responsePeerID,
			PublicKey: publicKey,
			Timestamp: timestamp,
			Nonce:     nonce,
			ChainID:   chainID,
		}

		now := time.Now().Unix()
		if abs(now-response.Timestamp) > AuthTimeout {
			return fmt.Errorf("response timestamp expired")
		}

		ln.challengeMu.RLock()
		originalChallenge, exists := ln.pendingChallenges[peerID]
		ln.challengeMu.RUnlock()

		if !exists {
			return fmt.Errorf("no pending challenge found")
		}

		if !bytesEqual(originalChallenge, response.Challenge) {
			return fmt.Errorf("challenge mismatch")
		}

		var pubKeyBytes []byte
		if len(response.PublicKey) == ed25519.PublicKeySize*2 {
			var err error
			pubKeyBytes, err = hex.DecodeString(response.PublicKey)
			if err != nil {
				return fmt.Errorf("failed to decode hex public key: %w", err)
			}
		} else if len(response.PublicKey) == ed25519.PublicKeySize {
			pubKeyBytes = []byte(response.PublicKey)
		} else {
			var err error
			pubKeyBytes, err = base64.StdEncoding.DecodeString(response.PublicKey)
			if err != nil {
				return fmt.Errorf("failed to decode base64 public key: %w", err)
			}
		}

		if len(pubKeyBytes) != ed25519.PublicKeySize {
			return fmt.Errorf("invalid public key size: got %d, expected %d", len(pubKeyBytes), ed25519.PublicKeySize)
		}

		if response.Version != AuthVersion {
			return fmt.Errorf("unsupported auth version: %s", response.Version)
		}

		if response.ChainID != DefaultChainID {
			logx.Warn("AUTH", "Different chain ID from peer ", peerID.String(), ": expected ", DefaultChainID, ", got ", response.ChainID)
		}

		dataToVerify := append(response.Challenge, []byte(fmt.Sprintf("%d", response.Nonce))...)
		dataToVerify = append(dataToVerify, []byte(response.ChainID)...)

		if !ed25519.Verify(pubKeyBytes, dataToVerify, response.Signature) {
			return fmt.Errorf("invalid signature")
		}

		ln.authMu.Lock()
		ln.authenticatedPeers[peerID] = &AuthenticatedPeer{
			PeerID:        peerID,
			PublicKey:     pubKeyBytes,
			AuthTimestamp: time.Now(),
			IsValid:       true,
		}
		ln.authMu.Unlock()

		ln.challengeMu.Lock()
		delete(ln.pendingChallenges, peerID)
		ln.challengeMu.Unlock()

		ln.listMu.Lock()
		if peerInfo, exists := ln.peers[peerID]; exists {
			peerInfo.IsAuthenticated = true
			peerInfo.AuthTimestamp = time.Now()
		}
		ln.listMu.Unlock()

		logx.Info("AUTH", "Authentication successful with ", peerID.String())
		return nil

	case "result":
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

func (ln *Libp2pNetwork) IsPeerAuthenticated(peerID peer.ID) bool {
	ln.authMu.RLock()
	defer ln.authMu.RUnlock()

	if authPeer, exists := ln.authenticatedPeers[peerID]; exists {
		return authPeer.IsValid
	}
	return false
}

func (ln *Libp2pNetwork) CleanupExpiredAuthentications() {
	ln.authMu.Lock()
	defer ln.authMu.Unlock()

	now := time.Now()
	expirationTime := now.Add(-time.Duration(AuthTimeout) * time.Second)

	for peerID, authPeer := range ln.authenticatedPeers {
		if authPeer.AuthTimestamp.Before(expirationTime) {
			if ln.host != nil && ln.host.Network().Connectedness(peerID) == network.Connected {
				authPeer.AuthTimestamp = now
			} else {
				delete(ln.authenticatedPeers, peerID)
			}
		}
	}
}

func (ln *Libp2pNetwork) RefreshAuthenticationForConnectedPeers() {
	ln.authMu.Lock()
	defer ln.authMu.Unlock()

	now := time.Now()
	refreshed := 0

	for peerID, authPeer := range ln.authenticatedPeers {
		if ln.host != nil && ln.host.Network().Connectedness(peerID) == network.Connected {
			authPeer.AuthTimestamp = now
			refreshed++
		}
	}

	if refreshed > 0 {
		logx.Debug("AUTH", "Refreshed authentication for ", refreshed, " connected peers")
	}
}

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
