package p2p

import (
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mezonai/mmn/logx"
)

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

func (ln *Libp2pNetwork) IsAllowed(peerID peer.ID) bool {
	ln.listMu.RLock()
	defer ln.listMu.RUnlock()

	if ln.blacklistEnabled {
		if ln.blacklist[peerID] {
			return false
		}
	}
	if ln.allowlistEnabled {
		if len(ln.allowlist) == 0 {
			return true
		}

		allowed := ln.allowlist[peerID]
		return allowed
	}

	return true
}

func (ln *Libp2pNetwork) AddToAllowlist(peerID peer.ID) {
	ln.listMu.Lock()
	defer ln.listMu.Unlock()

	if ln.allowlist == nil {
		ln.allowlist = make(map[peer.ID]bool)
	}

	if ln.allowlist[peerID] {
		return
	}

	ln.allowlist[peerID] = true
	logx.Info("ACCESS CONTROL", "Added peer to allowlist:", peerID.String())
}

func (ln *Libp2pNetwork) AddToBlacklist(peerID peer.ID) {
	ln.listMu.Lock()
	if ln.blacklist == nil {
		ln.blacklist = make(map[peer.ID]bool)
	}
	ln.blacklist[peerID] = true
	ln.listMu.Unlock()

	logx.Info("ACCESS CONTROL", "Added peer to blacklist:", peerID.String())

	if ln.host != nil && ln.host.Network().Connectedness(peerID) == network.Connected {
		err := ln.host.Network().ClosePeer(peerID)
		if err != nil {
			logx.Error("ACCESS CONTROL", "Failed to disconnect blacklisted peer:", err)
		} else {
			logx.Info("ACCESS CONTROL", "Disconnected blacklisted peer:", peerID.String())
		}
	}
}

func (ln *Libp2pNetwork) AddToBlacklistWithExpiry(peerID peer.ID, duration time.Duration) {
	ln.AddToBlacklist(peerID)
	if duration <= 0 {
		return
	}
	go func(id peer.ID, d time.Duration) {
		timer := time.NewTimer(d)
		<-timer.C
		ln.listMu.Lock()
		delete(ln.blacklist, id)
		ln.listMu.Unlock()
		logx.Info("ACCESS CONTROL", "Temporary blacklist expired for:", id.String())
	}(peerID, duration)
}

func (ln *Libp2pNetwork) EnableAllowlist(enabled bool) {
	ln.listMu.Lock()
	defer ln.listMu.Unlock()

	ln.allowlistEnabled = enabled
}

func (ln *Libp2pNetwork) EnableBlacklist(enabled bool) {
	ln.listMu.Lock()
	defer ln.listMu.Unlock()

	ln.blacklistEnabled = enabled
}

func (ln *Libp2pNetwork) UpdatePeerScore(peerID peer.ID, eventType string, value interface{}) {
	if ln.peerScoringManager != nil {
		ln.peerScoringManager.UpdatePeerScore(peerID, eventType, value)
	}
}

func (ln *Libp2pNetwork) GetPeerScore(peerID peer.ID) float64 {
	if ln.peerScoringManager != nil {
		return ln.peerScoringManager.GetPeerScore(peerID)
	}
	return 0.0
}

func (ln *Libp2pNetwork) GetTopPeers(n int) []*PeerScore {
	if ln.peerScoringManager != nil {
		return ln.peerScoringManager.GetTopPeers(n)
	}
	return nil
}

func (ln *Libp2pNetwork) GetPeerStats(peerID peer.ID) *PeerScore {
	if ln.peerScoringManager != nil {
		return ln.peerScoringManager.GetPeerStats(peerID)
	}
	return nil
}

func (ln *Libp2pNetwork) AutoAddToAllowlistIfBootstrap(peerID peer.ID) {
	ln.listMu.Lock()
	defer ln.listMu.Unlock()

	if ln.allowlistEnabled && len(ln.allowlist) < 3 {
		if !ln.allowlist[peerID] {
			ln.allowlist[peerID] = true
		}
	}
}
