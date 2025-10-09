package p2p

import (
	"context"
	"time"

	"github.com/mezonai/mmn/store"

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

// when no peers connected the blocks will not sync must run after 30s if synced stop sync
func (ln *Libp2pNetwork) startPeriodicSyncCheck(bs store.BlockStore) {
	// wait network setup
	time.Sleep(10 * time.Second)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ln.cleanupOldSyncRequests()
			// probe checkpoint every tick
			latest := bs.GetLatestFinalizedSlot()
			if latest >= MaxcheckpointScanBlocksRange {
				checkpoint := (latest / MaxcheckpointScanBlocksRange) * MaxcheckpointScanBlocksRange
				logx.Info("NETWORK:CHECKPOINT", "Probing checkpoint=", checkpoint, "latest=", latest)
				_ = ln.RequestCheckpointHash(context.Background(), checkpoint)
			}
		case <-ln.ctx.Done():
			return
		}
	}
}

func (ln *Libp2pNetwork) startCleanupRoutine() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ln.CleanupExpiredRequests()
			ln.cleanupOldMissingBlocksTracker()
			ln.cleanupOldRecentlyRequestedSlots()
			ln.cleanupBlockOrderingQueuePeriodic(ln.blockStore)
		case <-ln.ctx.Done():
			logx.Info("NETWORK:CLEANUP", "Stopping cleanup routine")
			return
		}
	}
}

func (ln *Libp2pNetwork) cleanupOldSyncRequests() {
	ln.syncMu.Lock()
	defer ln.syncMu.Unlock()

	now := time.Now()
	for requestID, info := range ln.activeSyncRequests {
		if !info.IsActive || now.Sub(info.StartTime) > 5*time.Minute {
			delete(ln.activeSyncRequests, requestID)
		}
	}
}

func (ln *Libp2pNetwork) cleanupOldRecentlyRequestedSlots() {
	ln.recentlyRequestedMu.Lock()
	defer ln.recentlyRequestedMu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)
	for slot, requestTime := range ln.recentlyRequestedSlots {
		if requestTime.Before(cutoff) {
			delete(ln.recentlyRequestedSlots, slot)
		}
	}
}
