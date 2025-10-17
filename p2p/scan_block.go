package p2p

import (
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/store"
)

func (ln *Libp2pNetwork) scanMissingBlocks(bs store.BlockStore) {
	latest := bs.GetLatestFinalizedSlot()
	if latest < 1 {
		logx.Info("NETWORK:SCAN", "No blocks in store, skipping scan")
		return
	}

	ln.scanMu.Lock()
	lastScannedSlot := ln.lastScannedSlot
	ln.scanMu.Unlock()

	var scanStart uint64
	if lastScannedSlot > 0 {
		scanStart = lastScannedSlot + 1
	} else {
		scanStart = 0
	}

	scanEnd := latest
	if scanEnd-scanStart > MaxcheckpointScanBlocksRange {
		scanEnd = scanStart + MaxcheckpointScanBlocksRange
	}

	logx.Info("NETWORK:SCAN", "Scanning for missing blocks from slot ", scanStart, " to ", scanEnd, " (range: ", scanEnd-scanStart+1, " slots)")

	var missingSlots []uint64
	var totalChecked int

	for slot := scanStart; slot <= scanEnd; slot++ {
		totalChecked++

		// Skip if block already exists
		if bs.Block(slot) != nil {
			continue
		}

		// Skip if already tracked as missing
		if ln.isSlotAlreadyTracked(slot) {
			continue
		}

		// Skip if already requested recently
		if ln.isSlotRecentlyRequested(slot) {
			logx.Debug("NETWORK:SCAN", "Slot", slot, "recently requested, skipping")
			continue
		}

		missingSlots = append(missingSlots, slot)

		// Track missing block
		ln.trackMissingBlock(slot)
	}

	// Update last scanned position
	ln.scanMu.Lock()
	ln.lastScannedSlot = scanEnd
	ln.scanMu.Unlock()

	// Enhanced logging
	ln.missingBlocksMu.RLock()
	trackedMissing := len(ln.missingBlocksTracker)
	ln.missingBlocksMu.RUnlock()
	logx.Info("NETWORK:SCAN", "Scan completed: checked", totalChecked, " slots, found ", len(missingSlots), " new missing blocks; tracked total:", trackedMissing)

	if len(missingSlots) > 0 {
		ln.requestMissingBlocks(missingSlots)
	}

	ln.checkRetryMissingBlocks(bs)
}

func (ln *Libp2pNetwork) requestMissingBlocks(missingSlots []uint64) {
	if len(missingSlots) == 0 {
		return
	}

	if len(ln.host.Network().Peers()) == 0 {
		return
	}

	missingBatchSize := int(SyncBlocksBatchSize)

	batchCount := 0
	for i := 0; i < len(missingSlots); i += missingBatchSize {
		batchCount++
		end := i + missingBatchSize
		if end > len(missingSlots) {
			end = len(missingSlots)
		}

		batch := missingSlots[i:end]
		fromSlot := batch[0]
		toSlot := batch[len(batch)-1]

		requestID := fmt.Sprintf("missing_blocks_%d_%d", fromSlot, toSlot)
		req := SyncRequest{
			RequestID: requestID,
			FromSlot:  fromSlot,
			ToSlot:    toSlot,
		}

		for _, slot := range batch {
			ln.markSlotAsRequested(slot)
		}

		// Filter out bootstrap peers
		allPeers := ln.host.Network().Peers()
		var compatiblePeers []peer.ID
		for _, p := range allPeers {
			if _, isBootstrap := ln.bootstrapPeerIDs[p]; isBootstrap {
				continue
			}
			compatiblePeers = append(compatiblePeers, p)
		}
		for _, targetPeer := range compatiblePeers {
			exception.SafeGoWithPanic("sendSyncRequestToPeer", func() {
				func(peer peer.ID) {
					if err := ln.sendSyncRequestToPeer(req, peer); err != nil {
						logx.Warn("NETWORK:SCAN", "Request scan fail to peer: ", peer.String(), " : ", err)
					}
				}(targetPeer)
			})
		}
	}

	logx.Info("NETWORK:SCAN", "Completed processing all ")
}

func (ln *Libp2pNetwork) shouldScanForMissingBlocks(bs store.BlockStore) bool {
	latest := bs.GetLatestFinalizedSlot()
	if latest < 1 {
		return false
	}

	ln.scanMu.RLock()
	lastScanned := ln.lastScannedSlot
	ln.scanMu.RUnlock()

	if lastScanned == 0 {
		return true
	}

	if latest > lastScanned {
		return true
	}

	return false
}

func (ln *Libp2pNetwork) checkRetryMissingBlocks(bs store.BlockStore) {
	ln.missingBlocksMu.Lock()
	defer ln.missingBlocksMu.Unlock()

	now := time.Now()
	var retrySlots []uint64

	for slot, info := range ln.missingBlocksTracker {
		// Skip if block was found
		if bs.Block(slot) != nil {
			delete(ln.missingBlocksTracker, slot)
			continue
		}

		// Check if it's time to retry
		if info.RetryCount < info.MaxRetries &&
			(info.LastRetry.IsZero() || now.Sub(info.LastRetry) > 30*time.Second) {
			retrySlots = append(retrySlots, slot)
			info.LastRetry = now
			info.RetryCount++
		}

		// Remove if max retries exceeded
		if info.RetryCount >= info.MaxRetries {
			delete(ln.missingBlocksTracker, slot)
		}
	}

	// Request retry blocks
	if len(retrySlots) > 0 {
		go ln.requestMissingBlocks(retrySlots)
	}
}

func (ln *Libp2pNetwork) removeFromMissingTracker(slot uint64) {
	ln.missingBlocksMu.Lock()
	defer ln.missingBlocksMu.Unlock()
	delete(ln.missingBlocksTracker, slot)
}

func (ln *Libp2pNetwork) trackMissingBlock(slot uint64) {
	ln.missingBlocksMu.Lock()
	defer ln.missingBlocksMu.Unlock()

	if _, exists := ln.missingBlocksTracker[slot]; !exists {
		ln.missingBlocksTracker[slot] = &MissingBlockInfo{
			Slot:       slot,
			FirstSeen:  time.Now(),
			LastRetry:  time.Time{},
			RetryCount: 0,
			MaxRetries: 1,
		}
	}
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
