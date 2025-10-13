package p2p

import (
	"encoding/json"
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
			return false
		}

		allowed := ln.allowlist[peerID]
		return allowed
	}

	return true
}

func (ln *Libp2pNetwork) IsBlacklisted(peerID peer.ID) bool {
	ln.listMu.RLock()
	defer ln.listMu.RUnlock()
	return ln.blacklistEnabled && ln.blacklist[peerID]
}

func (ln *Libp2pNetwork) AddToAllowlist(peerID peer.ID) {
	ln.listMu.Lock()
	if ln.allowlist == nil {
		ln.allowlist = make(map[peer.ID]bool)
	}

	if ln.allowlist[peerID] {
		ln.listMu.Unlock()
		return
	}

	ln.allowlist[peerID] = true
	ln.listMu.Unlock()

	ln.BroadcastAccessControlUpdate("allowlist", "add", peerID)
}

func (ln *Libp2pNetwork) AddToBlacklist(peerID peer.ID) {
	ln.listMu.Lock()
	if ln.blacklist == nil {
		ln.blacklist = make(map[peer.ID]bool)
	}
	ln.blacklist[peerID] = true
	ln.listMu.Unlock()

	ln.BroadcastAccessControlUpdate("blacklist", "add", peerID)

	if ln.host != nil && ln.host.Network().Connectedness(peerID) == network.Connected {
		err := ln.host.Network().ClosePeer(peerID)
		if err != nil {
			logx.Error("ACCESS CONTROL", "Failed to disconnect blacklisted peer:", err)
		}
	}
}

func (ln *Libp2pNetwork) AddToBlacklistWithExpiry(peerID peer.ID, duration time.Duration) {
	ln.AddToBlacklist(peerID)
	if duration <= 0 {
		return
	}
	// schedule removal after duration
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

func (ln *Libp2pNetwork) IsAllowlistEnabled() bool {
	ln.listMu.RLock()
	defer ln.listMu.RUnlock()
	return ln.allowlistEnabled
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

	if ln.host != nil && peerID == ln.host.ID() {
		return
	}

	if ln.allowlistEnabled {
		if !ln.allowlist[peerID] {
			ln.allowlist[peerID] = true
		}
	}
}

func (ln *Libp2pNetwork) BroadcastAccessControlUpdate(updateType, action string, peerID peer.ID) {
	if ln.topicAccessControl == nil {
		return
	}

	update := AccessControlUpdate{
		Type:      updateType, // blacklist || allowlist
		Action:    action,     // add || remove
		PeerID:    peerID.String(),
		Timestamp: time.Now().Unix(),
		NodeID:    ln.host.ID().String(),
	}

	data, err := json.Marshal(update)
	if err != nil {
		return
	}

	ln.topicAccessControl.Publish(ln.ctx, data)
}

func (ln *Libp2pNetwork) BroadcastAccessControlSync(updateType string, peerIDs []peer.ID) {
	if ln.topicAccessControlSync == nil {
		return
	}

	peerIDStrings := make([]string, len(peerIDs))
	for i, pid := range peerIDs {
		peerIDStrings[i] = pid.String()
	}

	sync := AccessControlSync{
		Type:       updateType, // blacklist || allowlist
		PeerIDs:    peerIDStrings,
		Timestamp:  time.Now().Unix(),
		NodeID:     ln.host.ID().String(),
		IsFullSync: true,
	}

	data, err := json.Marshal(sync)
	if err != nil {
		return
	}

	ln.topicAccessControlSync.Publish(ln.ctx, data)
}

func (ln *Libp2pNetwork) HandleAccessControlUpdate(update AccessControlUpdate) {
	if update.NodeID == ln.host.ID().String() {
		return
	}

	peerID, err := peer.Decode(update.PeerID)
	if err != nil {
		return
	}

	ln.listMu.Lock()
	defer ln.listMu.Unlock()

	switch update.Type {
	case "allowlist":
		if update.Action == "add" {
			if ln.allowlist == nil {
				ln.allowlist = make(map[peer.ID]bool)
			}
			if !ln.allowlist[peerID] {
				ln.allowlist[peerID] = true
			}
		} else if update.Action == "remove" {
			if ln.allowlist != nil {
				delete(ln.allowlist, peerID)
			}
		}
	case "blacklist":
		if update.Action == "add" {
			if ln.blacklist == nil {
				ln.blacklist = make(map[peer.ID]bool)
			}
			if !ln.blacklist[peerID] {
				ln.blacklist[peerID] = true

				if ln.host != nil && ln.host.Network().Connectedness(peerID) == network.Connected {
					ln.host.Network().ClosePeer(peerID)
				}
			}
		} else if update.Action == "remove" {
			if ln.blacklist != nil {
				delete(ln.blacklist, peerID)
			}
		}
	}
}

func (ln *Libp2pNetwork) HandleAccessControlSync(sync AccessControlSync) {
	if sync.NodeID == ln.host.ID().String() {
		return
	}

	ln.listMu.Lock()
	defer ln.listMu.Unlock()

	switch sync.Type {
	case "allowlist":
		if ln.allowlist == nil {
			ln.allowlist = make(map[peer.ID]bool)
		}
		for pid := range ln.allowlist {
			delete(ln.allowlist, pid)
		}
		for _, peerIDStr := range sync.PeerIDs {
			peerID, err := peer.Decode(peerIDStr)
			if err != nil {
				continue
			}
			ln.allowlist[peerID] = true
		}

	case "blacklist":
		if ln.blacklist == nil {
			ln.blacklist = make(map[peer.ID]bool)
		}
		for pid := range ln.blacklist {
			delete(ln.blacklist, pid)
		}
		for _, peerIDStr := range sync.PeerIDs {
			peerID, err := peer.Decode(peerIDStr)
			if err != nil {
				continue
			}
			ln.blacklist[peerID] = true
		}
	}
}
