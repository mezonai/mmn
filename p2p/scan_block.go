package p2p

import (
	"time"

	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/store"
)

func (ln *Libp2pNetwork) shouldCheckAroundSlot(bs store.BlockStore, slot uint64) bool {
	latest := bs.GetLatestFinalizedSlot()
	if latest < 1 {
		return false
	}

	if slot > latest {
		return false
	}

	ln.scanMu.RLock()
	lastScanned := ln.lastScannedSlot
	ln.scanMu.RUnlock()

	if lastScanned == 0 || slot > lastScanned {
		return true
	}

	return false
}

func (ln *Libp2pNetwork) checkForMissingBlocksAround(bs store.BlockStore, slot uint64, isCheckNext bool) {
	logx.Debug("NETWORK:SCAN", "check around slot =", slot)
	if !ln.shouldCheckAroundSlot(bs, slot) {
		logx.Debug("NETWORK:SCAN", "Skipping check around slot ", slot, " not needed")
		return
	}

	latest := bs.GetLatestFinalizedSlot()

	// Check previous slot (slot-1)
	if slot > 0 {
		prev := slot - 1
		if prev <= latest && bs.Block(prev) == nil && !ln.isSlotAlreadyTracked(prev) && !ln.isSlotRecentlyRequested(prev) {
			if ln.topicMissingBlockReq != nil {
				logx.Debug("NETWORK:SCAN", "request missing slot =", slot)
				ln.requestSingleMissingBlock(prev)
			}
		}
	}

	// Check next slot (slot+1)
	if isCheckNext {
		next := slot + 1
		if next <= latest && bs.Block(next) == nil && !ln.isSlotAlreadyTracked(next) && !ln.isSlotRecentlyRequested(next) {
			if ln.topicMissingBlockReq != nil {
				logx.Debug("NETWORK:SCAN", "request missing slot =", slot)
				ln.requestSingleMissingBlock(next)
			}
		}
	}

}

func (ln *Libp2pNetwork) requestSingleMissingBlock(slot uint64) {
	// mark before request to avoid duplicate spamming
	ln.markSlotAsRequested(slot)
	req := MissingBlockRequest{
		RequestID: GenerateSyncRequestID(),
		Slot:      slot,
	}
	data, err := jsonx.Marshal(req)
	if err != nil {
		logx.Warn("NETWORK:MISSING", "marshal request error:", err)
		return
	}
	if err := ln.topicMissingBlockReq.Publish(ln.ctx, data); err != nil {
		logx.Warn("NETWORK:MISSING", "publish request error:", err)
	}
}

func (ln *Libp2pNetwork) incrementMissingRetry(slot uint64) bool {
	ln.missingBlocksMu.Lock()
	defer ln.missingBlocksMu.Unlock()

	info, exists := ln.missingBlocksTracker[slot]
	if !exists {
		ln.missingBlocksTracker[slot] = &MissingBlockInfo{
			Slot:       slot,
			FirstSeen:  time.Now(),
			LastRetry:  time.Now(),
			RetryCount: 0,
			MaxRetries: MaxMissingRetry,
		}
		return true
	}

	if ln.blockStore.Block(slot) != nil {
		delete(ln.missingBlocksTracker, slot)
		return false
	}

	info.RetryCount++
	info.LastRetry = time.Now()

	logx.Warn("NETWORK:SCAN", "requestmissing retry for slot =", slot, "retry count =", info.RetryCount)

	if info.RetryCount >= info.MaxRetries {
		if slot == ln.nextExpectedSlotForQueue {
			ln.nextExpectedSlotForQueue = slot + 1
		}
		delete(ln.missingBlocksTracker, slot)
		return false
	}
	return true
}

func (ln *Libp2pNetwork) removeFromMissingTracker(slot uint64) {
	ln.missingBlocksMu.Lock()
	defer ln.missingBlocksMu.Unlock()
	delete(ln.missingBlocksTracker, slot)
}

func (ln *Libp2pNetwork) isSlotAlreadyTracked(slot uint64) bool {
	ln.missingBlocksMu.RLock()
	defer ln.missingBlocksMu.RUnlock()

	_, exists := ln.missingBlocksTracker[slot]
	return exists
}

func (ln *Libp2pNetwork) isSlotRecentlyRequested(slot uint64) bool {
	ln.recentlyRequestedMu.RLock()
	defer ln.recentlyRequestedMu.RUnlock()

	if requestTime, exists := ln.recentlyRequestedSlots[slot]; exists {
		if time.Since(requestTime) < 5*time.Minute {
			return true
		}
		delete(ln.recentlyRequestedSlots, slot)
	}
	return false
}

func (ln *Libp2pNetwork) markSlotAsRequested(slot uint64) {
	ln.recentlyRequestedMu.Lock()
	defer ln.recentlyRequestedMu.Unlock()

	ln.recentlyRequestedSlots[slot] = time.Now()
}

func (ln *Libp2pNetwork) cleanupOldMissingBlocksTracker() {
	ln.missingBlocksMu.Lock()
	defer ln.missingBlocksMu.Unlock()

	now := time.Now()
	cutoff := now.Add(-10 * time.Minute) // Remove entries older than 10 minutes

	var toRemove []uint64
	for slot, info := range ln.missingBlocksTracker {
		if info.FirstSeen.Before(cutoff) {
			toRemove = append(toRemove, slot)
		}
	}

	for _, slot := range toRemove {
		delete(ln.missingBlocksTracker, slot)
	}
}
