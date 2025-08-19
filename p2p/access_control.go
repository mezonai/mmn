package p2p

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mezonai/mmn/logx"
)

// InitializeAccessControl initializes the allowlist and blacklist maps
func (ln *Libp2pNetwork) InitializeAccessControl() {
	ln.listMu.Lock()
	defer ln.listMu.Unlock()

	if ln.allowlist == nil {
		ln.allowlist = make(map[peer.ID]bool)
	}
	if ln.blacklist == nil {
		ln.blacklist = make(map[peer.ID]bool)
	}

	logx.Info("ACCESS CONTROL", "Initialized allowlist and blacklist")
}

// IsAllowed checks if a peer is allowed to connect based on allowlist and blacklist rules
func (ln *Libp2pNetwork) IsAllowed(peerID peer.ID) bool {
	ln.listMu.RLock()
	defer ln.listMu.RUnlock()

	// Check blacklist first (highest priority)
	if ln.blacklistEnabled {
		if ln.blacklist[peerID] {
			logx.Info("ACCESS CONTROL", "Peer blocked by blacklist:", peerID.String())
			return false
		}
	}

	// Check allowlist
	if ln.allowlistEnabled {
		// If allowlist is empty, allow all peers (bootstrap mode)
		if len(ln.allowlist) == 0 {
			logx.Info("ACCESS CONTROL", "Allowlist is empty - allowing peer for bootstrap:", peerID.String())
			return true
		}

		allowed := ln.allowlist[peerID]
		if !allowed {
			logx.Info("ACCESS CONTROL", "Peer not in allowlist:", peerID.String())
		}
		return allowed
	}

	// If no allowlist enabled and not blacklisted -> allow
	return true
}

// IsBlacklisted checks if a peer is in the blacklist
func (ln *Libp2pNetwork) IsBlacklisted(peerID peer.ID) bool {
	ln.listMu.RLock()
	defer ln.listMu.RUnlock()

	return ln.blacklistEnabled && ln.blacklist[peerID]
}

// AddToAllowlist adds a peer to the allowlist
func (ln *Libp2pNetwork) AddToAllowlist(peerID peer.ID) {
	ln.listMu.Lock()
	defer ln.listMu.Unlock()

	if ln.allowlist == nil {
		ln.allowlist = make(map[peer.ID]bool)
	}

	ln.allowlist[peerID] = true
	logx.Info("ACCESS CONTROL", "Added peer to allowlist:", peerID.String())
}

// RemoveFromAllowlist removes a peer from the allowlist
func (ln *Libp2pNetwork) RemoveFromAllowlist(peerID peer.ID) {
	ln.listMu.Lock()
	defer ln.listMu.Unlock()

	delete(ln.allowlist, peerID)
	logx.Info("ACCESS CONTROL", "Removed peer from allowlist:", peerID.String())
}

// AddToBlacklist adds a peer to the blacklist and disconnects if currently connected
func (ln *Libp2pNetwork) AddToBlacklist(peerID peer.ID) {
	ln.listMu.Lock()
	if ln.blacklist == nil {
		ln.blacklist = make(map[peer.ID]bool)
	}
	ln.blacklist[peerID] = true
	ln.listMu.Unlock()

	logx.Info("ACCESS CONTROL", "Added peer to blacklist:", peerID.String())

	// Disconnect if currently connected (only if host is available)
	if ln.host != nil && ln.host.Network().Connectedness(peerID) == 1 { // Connected
		err := ln.host.Network().ClosePeer(peerID)
		if err != nil {
			logx.Error("ACCESS CONTROL", "Failed to disconnect blacklisted peer:", err)
		} else {
			logx.Info("ACCESS CONTROL", "Disconnected blacklisted peer:", peerID.String())
		}
	}
}

// RemoveFromBlacklist removes a peer from the blacklist
func (ln *Libp2pNetwork) RemoveFromBlacklist(peerID peer.ID) {
	ln.listMu.Lock()
	defer ln.listMu.Unlock()

	delete(ln.blacklist, peerID)
	logx.Info("ACCESS CONTROL", "Removed peer from blacklist:", peerID.String())
}

// EnableAllowlist enables or disables the allowlist functionality
func (ln *Libp2pNetwork) EnableAllowlist(enabled bool) {
	ln.listMu.Lock()
	defer ln.listMu.Unlock()

	ln.allowlistEnabled = enabled
	if enabled {
		logx.Info("ACCESS CONTROL", "Allowlist enabled")
	} else {
		logx.Info("ACCESS CONTROL", "Allowlist disabled")
	}
}

// EnableBlacklist enables or disables the blacklist functionality
func (ln *Libp2pNetwork) EnableBlacklist(enabled bool) {
	ln.listMu.Lock()
	defer ln.listMu.Unlock()

	ln.blacklistEnabled = enabled
	if enabled {
		logx.Info("ACCESS CONTROL", "Blacklist enabled")
	} else {
		logx.Info("ACCESS CONTROL", "Blacklist disabled")
	}
}

// GetAllowlistStatus returns the current allowlist configuration
func (ln *Libp2pNetwork) GetAllowlistStatus() (enabled bool, count int) {
	ln.listMu.RLock()
	defer ln.listMu.RUnlock()

	return ln.allowlistEnabled, len(ln.allowlist)
}

// GetBlacklistStatus returns the current blacklist configuration
func (ln *Libp2pNetwork) GetBlacklistStatus() (enabled bool, count int) {
	ln.listMu.RLock()
	defer ln.listMu.RUnlock()

	return ln.blacklistEnabled, len(ln.blacklist)
}

// GetAllowedPeers returns a copy of the allowlist
func (ln *Libp2pNetwork) GetAllowedPeers() []peer.ID {
	ln.listMu.RLock()
	defer ln.listMu.RUnlock()

	peers := make([]peer.ID, 0, len(ln.allowlist))
	for peerID := range ln.allowlist {
		peers = append(peers, peerID)
	}
	return peers
}

// GetBlacklistedPeers returns a copy of the blacklist
func (ln *Libp2pNetwork) GetBlacklistedPeers() []peer.ID {
	ln.listMu.RLock()
	defer ln.listMu.RUnlock()

	peers := make([]peer.ID, 0, len(ln.blacklist))
	for peerID := range ln.blacklist {
		peers = append(peers, peerID)
	}
	return peers
}

// ClearAllowlist removes all peers from the allowlist
func (ln *Libp2pNetwork) ClearAllowlist() {
	ln.listMu.Lock()
	defer ln.listMu.Unlock()

	ln.allowlist = make(map[peer.ID]bool)
	logx.Info("ACCESS CONTROL", "Cleared allowlist")
}

// ClearBlacklist removes all peers from the blacklist
func (ln *Libp2pNetwork) ClearBlacklist() {
	ln.listMu.Lock()
	defer ln.listMu.Unlock()

	ln.blacklist = make(map[peer.ID]bool)
	logx.Info("ACCESS CONTROL", "Cleared blacklist")
}

// UpdatePeerScore updates the peer scoring system with an event
func (ln *Libp2pNetwork) UpdatePeerScore(peerID peer.ID, eventType string, value interface{}) {
	if ln.peerScoringManager != nil {
		ln.peerScoringManager.UpdatePeerScore(peerID, eventType, value)
	}
}

// GetPeerScore returns the current score for a peer
func (ln *Libp2pNetwork) GetPeerScore(peerID peer.ID) float64 {
	if ln.peerScoringManager != nil {
		return ln.peerScoringManager.GetPeerScore(peerID)
	}
	return 0.0
}

// GetTopPeers returns the top N peers by score
func (ln *Libp2pNetwork) GetTopPeers(n int) []*PeerScore {
	if ln.peerScoringManager != nil {
		return ln.peerScoringManager.GetTopPeers(n)
	}
	return nil
}

// GetPeerStats returns detailed statistics for a peer
func (ln *Libp2pNetwork) GetPeerStats(peerID peer.ID) *PeerScore {
	if ln.peerScoringManager != nil {
		return ln.peerScoringManager.GetPeerStats(peerID)
	}
	return nil
}

// AutoAddToAllowlistIfEmpty automatically adds authenticated peers to allowlist if it's empty
func (ln *Libp2pNetwork) AutoAddToAllowlistIfEmpty(peerID peer.ID) {
	ln.listMu.Lock()
	defer ln.listMu.Unlock()
	
	// Only auto-add if allowlist is enabled and currently empty
	if ln.allowlistEnabled && len(ln.allowlist) == 0 {
		ln.allowlist[peerID] = true
		logx.Info("ACCESS CONTROL", "Auto-added first authenticated peer to allowlist:", peerID.String())
	}
}

// AutoAddToAllowlistIfBootstrap automatically adds peers to allowlist during bootstrap phase
func (ln *Libp2pNetwork) AutoAddToAllowlistIfBootstrap(peerID peer.ID) {
	ln.listMu.Lock()
	defer ln.listMu.Unlock()
	
	// Auto-add if allowlist is enabled but has very few peers (bootstrap phase)
	if ln.allowlistEnabled && len(ln.allowlist) < 3 {
		if !ln.allowlist[peerID] {
			ln.allowlist[peerID] = true
			logx.Info("ACCESS CONTROL", "Auto-added peer to allowlist during bootstrap:", peerID.String())
		}
	}
}
