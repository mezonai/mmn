package p2p

import (
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

func NewSyncRequestTracker(requestID string, fromSlot, toSlot uint64) *SyncRequestTracker {
	return &SyncRequestTracker{
		RequestID: requestID,
		FromSlot:  fromSlot,
		ToSlot:    toSlot,
		StartTime: time.Now(),
		IsActive:  false,
	}
}

func (t *SyncRequestTracker) ActivatePeer(peerID peer.ID, stream network.Stream) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.IsActive {
		return false
	}

	t.ActivePeer = peerID
	t.ActiveStream = stream
	t.IsActive = true
	return true
}

func (t *SyncRequestTracker) IsActiveRequest() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.IsActive
}

func (t *SyncRequestTracker) GetActivePeer() peer.ID {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.ActivePeer
}

func (t *SyncRequestTracker) CloseRequest() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ActiveStream != nil {
		t.ActiveStream.Close()
	}
	t.IsActive = false
	t.ActivePeer = ""
	t.ActiveStream = nil
}
