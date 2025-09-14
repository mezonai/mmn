package consensus

import (
	"fmt"
	"sync"

	"github.com/mezonai/mmn/logx"
)

type Collector struct {
	// Sharded locks for better concurrency
	shardMutexes []sync.RWMutex
	shardCount   int

	// Sharded vote storage
	shardVotes []map[uint64]map[string]*Vote // slot → voterID → Vote

	total     int // total number of validators
	threshold int // threshold number of votes (2f+1)
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
	return &Collector{
		shardMutexes: shardMutexes,
		shardCount:   shardCount,
		shardVotes:   shardVotes,
		total:        n,
		threshold:    q,
	}
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
