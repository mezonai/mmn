package p2p

import (
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mezonai/mmn/logx"
)

func NewSyncRequestTracker(requestID string, fromSlot, toSlot uint64) *SyncRequestTracker {
	return &SyncRequestTracker{
		RequestID: requestID,
		FromSlot:  fromSlot,
		ToSlot:    toSlot,
		StartTime: time.Now(),
		IsActive:  false,
		AllPeers:  make(map[peer.ID]network.Stream),
	}
}

func (t *SyncRequestTracker) ActivatePeer(peerID peer.ID, stream network.Stream) bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.AllPeers[peerID] = stream

	if t.IsActive {
		return false
	}

	t.ActivePeer = peerID
	t.ActiveStream = stream
	t.IsActive = true
	return true
}

func (t *SyncRequestTracker) CloseRequest() {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.ActiveStream != nil {
		t.ActiveStream.Close()
	}
	t.IsActive = false
	t.ActivePeer = ""
	t.ActiveStream = nil
}

// CloseAllOtherPeers closes all peer streams except the active one
func (t *SyncRequestTracker) CloseAllOtherPeers() {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	for peerID, stream := range t.AllPeers {
		if peerID != t.ActivePeer && stream != nil {
			stream.Close()
		}
	}
	// Clear all peers after closing
	t.AllPeers = make(map[peer.ID]network.Stream)
}

// CloseAllPeers closes all peer streams including the active one
func (t *SyncRequestTracker) CloseAllPeers() {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Close active stream
	if t.ActiveStream != nil {
		t.ActiveStream.Close()
	}

	// Close all other peer streams
	for _, stream := range t.AllPeers {
		if stream != nil {
			stream.Close()
		}
	}

	t.IsActive = false
	t.ActivePeer = ""
	t.ActiveStream = nil
	t.AllPeers = make(map[peer.ID]network.Stream)
}

func (ln *Libp2pNetwork) startCleanupRoutine() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ln.CleanupExpiredRequests()
		case <-ln.ctx.Done():
			logx.Info("NETWORK:CLEANUP", "Stopping cleanup routine")
			return
		}
	}
}
