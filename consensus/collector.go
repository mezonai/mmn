package consensus

import (
	"fmt"
	"sync"
)

type Collector struct {
	mu              sync.Mutex
	votes           map[uint64]map[string]*Vote // slot → voterID → Vote
	total           int                         // total number of validators
	threshold       int                         // threshold number of votes for simple majority
	activeVoters    map[string]bool             // track active voters who have voted recently
	recentSlots     []uint64                    // track recent slots for dynamic counting
}

func NewCollector(n int) *Collector {
	// Use simple majority: (n/2 + 1) for better performance
	// This allows 1/2 + 1 nodes to confirm a block
	threshold := n/2 + 1
	if threshold < 1 {
		threshold = 1 // At least 1 vote needed
	}
	fmt.Printf("[collector] total=%d threshold=%d (simple majority)\n", n, threshold)
	return &Collector{
		votes:        make(map[uint64]map[string]*Vote),
		total:        n,
		threshold:    threshold,
		activeVoters: make(map[string]bool),
		recentSlots:  make([]uint64, 0),
	}
}

// UpdateValidatorCount dynamically updates the validator count and threshold
func (c *Collector) UpdateValidatorCount(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	oldTotal := c.total
	oldThreshold := c.threshold
	
	c.total = n
	c.threshold = n/2 + 1
	if c.threshold < 1 {
		c.threshold = 1
	}
	
	fmt.Printf("[collector] updated validators: %d→%d, threshold: %d→%d\n", 
		oldTotal, c.total, oldThreshold, c.threshold)
}

// updateDynamicThreshold calculates threshold based on active voters
func (c *Collector) updateDynamicThreshold() {
	activeCount := len(c.activeVoters)
	if activeCount > c.total {
		c.total = activeCount
		c.threshold = activeCount/2 + 1
		fmt.Printf("[collector] auto-updated from active voters: total=%d threshold=%d\n", 
			c.total, c.threshold)
	}
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
	
	// Check for duplicate vote - log warning instead of error for better debugging
	if existingVote, exists := slotVotes[v.VoterID]; exists {
		// If it's the exact same vote, ignore silently (network duplicate)
		if existingVote.BlockHash == v.BlockHash {
			fmt.Printf("[collector] duplicate vote ignored from %s for slot %d (same block)\n", v.VoterID, v.Slot)
			count := len(slotVotes)
			return count >= c.threshold, false, nil
		}
		// Different block hash is a real conflict
		return false, false, fmt.Errorf("conflicting vote from %s for slot %d: existing=%x new=%x", 
			v.VoterID, v.Slot, existingVote.BlockHash[:8], v.BlockHash[:8])
	}
	
	slotVotes[v.VoterID] = v
	count := len(slotVotes)
	
	// Track active voters for dynamic threshold
	c.activeVoters[v.VoterID] = true
	c.recentSlots = append(c.recentSlots, v.Slot)
	
	// Periodically update threshold based on active voters
	if len(c.recentSlots) > 10 {
		c.updateDynamicThreshold()
		c.recentSlots = c.recentSlots[5:] // keep last 5 slots
	}
	
	fmt.Printf("[collector] slot=%d votes=%d/%d (total_validators=%d active_voters=%d)\n", 
		v.Slot, count, c.threshold, c.total, len(c.activeVoters))
		
	if count >= c.threshold {
		// Check if this is the first time we reach threshold
		wasAlreadyCommitted := (count - 1) >= c.threshold
		return true, !wasAlreadyCommitted, nil
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

// GetThreshold returns current threshold for external monitoring
func (c *Collector) GetThreshold() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.threshold
}

// GetTotalValidators returns current total validator count
func (c *Collector) GetTotalValidators() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

// CleanupOldVotes removes votes for slots older than the given slot to prevent memory leak
func (c *Collector) CleanupOldVotes(currentSlot uint64, keepSlots uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if currentSlot <= keepSlots {
		return
	}
	
	cutoffSlot := currentSlot - keepSlots
	removed := 0
	for slot := range c.votes {
		if slot < cutoffSlot {
			delete(c.votes, slot)
			removed++
		}
	}
	
	// Also cleanup old active voters if they haven't voted recently
	if len(c.recentSlots) > 0 {
		minRecentSlot := c.recentSlots[0]
		if minRecentSlot > keepSlots {
			// Reset active voters periodically to adapt to network changes
			oldCount := len(c.activeVoters)
			c.activeVoters = make(map[string]bool)
			if oldCount > 0 {
				fmt.Printf("[collector] reset %d active voters to adapt to network changes\n", oldCount)
			}
		}
	}
	
	if removed > 0 {
		fmt.Printf("[collector] cleaned up %d old vote slots (keeping last %d slots)\n", removed, keepSlots)
	}
}
