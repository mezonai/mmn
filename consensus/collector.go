package consensus

import (
	"fmt"
	"log"
	"sync"
)

// Collector lưu các vote và kiểm quorum (2f+1)
type Collector struct {
	mu        sync.Mutex
	votes     map[uint64]map[string]*Vote // slot → voterID → Vote
	total     int                         // tổng số validator
	threshold int                         // số vote cần thiết (2f+1)
}

// NewCollector khởi với n validator
func NewCollector(n int) *Collector {
	f := (n - 1) / 3
	q := 2*f + 1
	return &Collector{
		votes:     make(map[uint64]map[string]*Vote),
		total:     n,
		threshold: q,
	}
}

// AddVote thêm vote vào bộ đếm, trả về true nếu đạt quorum
func (c *Collector) AddVote(v *Vote) (bool, error) {
	if err := v.Validate(); err != nil {
		return false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	slotVotes, ok := c.votes[v.Slot]
	if !ok {
		slotVotes = make(map[string]*Vote)
		c.votes[v.Slot] = slotVotes
	}
	if _, exists := slotVotes[v.VoterID]; exists {
		return false, fmt.Errorf("duplicate vote from %s for slot %d", v.VoterID, v.Slot)
	}
	slotVotes[v.VoterID] = v

	count := len(slotVotes)
	log.Printf("[collector] slot=%d votes=%d/%d", v.Slot, count, c.threshold)
	if count >= c.threshold {
		return true, nil
	}
	return false, nil
}

// VotesForSlot trả về bản copy các vote đã lưu cho slot
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
