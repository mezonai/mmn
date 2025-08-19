package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mezonai/mmn/logx"
)

// AuthMiddleware wraps a stream handler with authentication checks
func (ln *Libp2pNetwork) AuthMiddleware(handler func(network.Stream)) func(network.Stream) {
	return func(s network.Stream) {
		remotePeer := s.Conn().RemotePeer()

		// Check if peer is authenticated
		if !ln.IsPeerAuthenticated(remotePeer) {
			logx.Error("AUTH:MIDDLEWARE", "Unauthenticated peer: ", remotePeer.String())

			errorMsg := map[string]interface{}{
				"error": "Authentication required",
				"code":  "UNAUTHENTICATED",
			}

			errorData, err := json.Marshal(errorMsg)
			if err == nil {
				s.Write(errorData)
			}

			s.Close()
			return
		}

		// Peer is authenticated, proceed with handler
		handler(s)
	}
}

// RequireAuth is a helper function to check authentication before proceeding
func (ln *Libp2pNetwork) RequireAuth(peerID peer.ID) error {
	if !ln.IsPeerAuthenticated(peerID) {
		return fmt.Errorf("peer %s is not authenticated", peerID.String())
	}
	return nil
}

// GetPeerAuthStatus returns the authentication status of a peer
func (ln *Libp2pNetwork) GetPeerAuthStatus(peerID peer.ID) (bool, error) {
	ln.authMu.RLock()
	defer ln.authMu.RUnlock()

	if authPeer, exists := ln.authenticatedPeers[peerID]; exists {
		return authPeer.IsValid, nil
	}
	return false, fmt.Errorf("peer %s not found in authenticated peers", peerID.String())
}

// AuthenticatePeerWithRetry attempts authentication with retry logic
func (ln *Libp2pNetwork) AuthenticatePeerWithRetry(ctx context.Context, peerID peer.ID, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		if err := ln.InitiateAuthentication(ctx, peerID); err == nil {
			logx.Info("AUTH:RETRY", "Authentication successful ", i+1)
			return nil
		} else {
			logx.Warn("AUTH:RETRY", "Authentication attempt: ", err.Error())
			if i < maxRetries-1 {
				// Wait before retry (exponential backoff)
				waitTime := time.Duration(1<<uint(i)) * time.Second
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(waitTime):
					continue
				}
			}
		}
	}
	return fmt.Errorf("authentication failed after %d attempts", maxRetries)
}
