package consensus

import (
	"fmt"
	"sync"
	"time"

	"github.com/mezonai/mmn/logx"
)

// Vote cleanup constants - these are optimization settings and don't affect consensus
const (
	DefaultVoteRetentionSlots  = 1500             // Keep votes for last 1500 slots ~ 10 minutes
	DefaultVoteCleanupInterval = 10 * time.Minute // Cleanup every 10 minutes
)

type Collector struct {
	// Sharded locks for better concurrency
	shardMutexes []sync.RWMutex
	shardCount   int

	// Sharded vote storage
	shardVotes []map[uint64]map[string]*Vote // slot → voterID → Vote

	total     int    // total number of validators
	threshold int    // threshold number of votes (2f+1)
	maxSlot   uint64 // highest slot number seen (for cleanup optimization)
}

func NewCollector(n int) *Collector {
	f := n / 3
	q := 2 * f
	shardCount := 8 // Use 8 shards for better concurrency

	// Initialize sharded storage
	shardMutexes := make([]sync.RWMutex, shardCount)
	shardVotes := make([]map[uint64]map[string]*Vote, shardCount)

	for i := 0; i < shardCount; i++ {
		shardVotes[i] = make(map[uint64]map[string]*Vote)
	}

	logx.Info("CONSENSUS", fmt.Sprintf("total=%d threshold=%d shards=%d", n, q, shardCount))
	collector := &Collector{
		shardMutexes: shardMutexes,
		shardCount:   shardCount,
		shardVotes:   shardVotes,
		total:        n,
		threshold:    q,
		maxSlot:      0,
	}
	// Start periodic cleanup to prevent memory leak
	collector.StartPeriodicCleanup(DefaultVoteRetentionSlots, DefaultVoteCleanupInterval)
	return collector
}

// getShard returns the shard index for a given slot
func (c *Collector) getShard(slot uint64) int {
	return int(slot) % c.shardCount
}

// return (committed, need apply block, err)
func (c *Collector) AddVote(v *Vote) (bool, bool, error) {
	if err := v.Validate(); err != nil {
		return false, false, err
	}

	shardIndex := c.getShard(v.Slot)
	c.shardMutexes[shardIndex].Lock()
	defer c.shardMutexes[shardIndex].Unlock()

	slotVotes, ok := c.shardVotes[shardIndex][v.Slot]
	if !ok {
		slotVotes = make(map[string]*Vote)
		c.shardVotes[shardIndex][v.Slot] = slotVotes
	}
	if _, exists := slotVotes[v.VoterID]; exists {
		return false, false, fmt.Errorf("duplicate vote from %s for slot %d", v.VoterID, v.Slot)
	}
	slotVotes[v.VoterID] = v

	// Update maxSlot for cleanup optimization
	if v.Slot > c.maxSlot {
		c.maxSlot = v.Slot
	}

	count := len(slotVotes)
	logx.Info("CONSENSUS", fmt.Sprintf("slot=%d votes=%d/%d shard=%d", v.Slot, count, c.threshold, shardIndex))
	if count >= c.threshold {
		return true, true, nil
	}
	return false, false, nil
}

func (c *Collector) VotesForSlot(slot uint64) map[string]*Vote {
	shardIndex := c.getShard(slot)
	c.shardMutexes[shardIndex].RLock()
	defer c.shardMutexes[shardIndex].RUnlock()

	res := make(map[string]*Vote)
	if m, ok := c.shardVotes[shardIndex][slot]; ok {
		for id, v := range m {
			res[id] = v
		}
	}
	return res
}

// CleanupOldVotes removes votes for slots older than the specified threshold
// to prevent memory leak from accumulating votes indefinitely
func (c *Collector) CleanupOldVotes(currentSlot uint64, keepRecentSlots uint64) {
	if currentSlot < keepRecentSlots {
		return // Not enough slots to cleanup
	}

	cleanupThreshold := currentSlot - keepRecentSlots
	deletedCount := 0
	// Iterate shards to clean old slots
	for si := 0; si < c.shardCount; si++ {
		c.shardMutexes[si].Lock()
		for slot := range c.shardVotes[si] {
			if slot < cleanupThreshold {
				delete(c.shardVotes[si], slot)
				deletedCount++
			}
		}
		c.shardMutexes[si].Unlock()
	}
	if deletedCount > 0 {
		logx.Info("CONSENSUS", fmt.Sprintf("Cleaned up votes for %d old slots (older than slot %d)", deletedCount, cleanupThreshold))
	}
}

// StartPeriodicCleanup starts a background goroutine that periodically cleans up old votes
func (c *Collector) StartPeriodicCleanup(keepRecentSlots uint64, cleanupInterval time.Duration) {
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for range ticker.C {
			// Use maxSlot instead of iterating through all votes
			currentSlot := c.maxSlot

			if currentSlot > 0 {
				c.CleanupOldVotes(currentSlot, keepRecentSlots)
			}
		}
	}()
}
