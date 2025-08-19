package consensus

import (
	"fmt"
	"sync"
)

type Collector struct {
	mu        sync.Mutex
	votes     map[uint64]map[string]*Vote // slot → voterID → Vote
	total     int                         // total number of validators
	threshold int                         // threshold number of votes (2f+1)
}

func NewCollector(n int) *Collector {
	// Use proper BFT threshold: (2f+1) where f is max Byzantine faults
	// For practical single-node consensus, threshold should be 1
	threshold := (n*2 + 2) / 3 // Same as cmd/node.go calculation
	fmt.Printf("[collector] total=%d threshold=%d\n", n, threshold)
	return &Collector{
		votes:     make(map[uint64]map[string]*Vote),
		total:     n,
		threshold: threshold,
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
	if _, exists := slotVotes[v.VoterID]; exists {
		return false, false, fmt.Errorf("duplicate vote from %s for slot %d", v.VoterID, v.Slot)
	}
	slotVotes[v.VoterID] = v

	count := len(slotVotes)
	fmt.Printf("[collector] slot=%d votes=%d/%d\n", v.Slot, count, c.threshold)
	if count >= c.threshold {
		if count-1 >= c.threshold {
			return true, false, nil
		}
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
