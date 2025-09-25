package consensus

import (
	"fmt"
	"sync"
	"time"

	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/logx"
)

// Vote cleanup constants - these are optimization settings and don't affect consensus
const (
	DefaultVoteRetentionSlots  = 1500             // Keep votes for last 1500 slots ~ 10 minutes
	DefaultVoteCleanupInterval = 10 * time.Minute // Cleanup every 10 minutes
)

type Collector struct {
	mu        sync.Mutex
	votes     map[uint64]map[string]*Vote // slot → voterID → Vote
	total     int                         // total number of validators
	threshold int                         // threshold number of votes (2f+1)
	maxSlot   uint64                      // highest slot number seen (for cleanup optimization)
}

func NewCollector(n int) *Collector {
	f := n / 3
	q := 2 * f
	logx.Info("CONSENSUS", fmt.Sprintf("total=%d threshold=%d", n, q))
	collector := &Collector{
		votes:     make(map[uint64]map[string]*Vote),
		total:     n,
		threshold: q,
		maxSlot:   0,
	}
	// Start periodic cleanup to prevent memory leak
	exception.SafeGo("StartPeriodicCleanup", func() {
		collector.StartPeriodicCleanup(DefaultVoteRetentionSlots, DefaultVoteCleanupInterval)
	})
	return collector
}

// return (committed, need apply block, err)
func (c *Collector) AddVote(v *Vote) (bool, bool, error) {
	if err := v.Validate(); err != nil {
		return false, false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	slotVotes, ok := c.votes[v.Slot]
	if !ok {
		slotVotes = make(map[string]*Vote)
		c.votes[v.Slot] = slotVotes
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
	logx.Info("CONSENSUS", fmt.Sprintf("slot=%d votes=%d/%d", v.Slot, count, c.threshold))
	if count >= c.threshold {
		// if count-1 >= c.threshold { error in case block receive at vote 3/2 => onVote 1/2 2/2 (but dont have block)
		// 	return true, false, nil
		// }
		return true, true, nil
	}
	return false, false, nil
}

func (c *Collector) VotesForSlot(slot uint64) map[string]*Vote {
	c.mu.Lock()
	defer c.mu.Unlock()
	res := make(map[string]*Vote)
	if m, ok := c.votes[slot]; ok {
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
	var slotsToDelete []uint64
	for slot := range c.votes {
		if slot < cleanupThreshold {
			slotsToDelete = append(slotsToDelete, slot)
		}
	}

	// Delete collected slots with minimal locking
	if len(slotsToDelete) > 0 {
		deletedCount := 0
		for _, slot := range slotsToDelete {
			delete(c.votes, slot)
			deletedCount++
		}

		if deletedCount > 0 {
			logx.Info("CONSENSUS", fmt.Sprintf("Cleaned up votes for %d old slots (older than slot %d)", deletedCount, cleanupThreshold))
		}
	}
}

// StartPeriodicCleanup starts a background goroutine that periodically cleans up old votes
func (c *Collector) StartPeriodicCleanup(keepRecentSlots uint64, cleanupInterval time.Duration) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		// Use maxSlot instead of iterating through all votes
		currentSlot := c.maxSlot

		if currentSlot > 0 {
			exception.SafeGo("CleanupOldVotes", func() {
				c.CleanupOldVotes(currentSlot, keepRecentSlots)
			})
		}
	}
}
