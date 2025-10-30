package faucet

import (
	"github.com/libp2p/go-libp2p/core/peer"
)

type FaucetVoteCollector struct {
	voters    map[string]map[peer.ID]bool
	threshold int
}

func NewFaucetVoteCollector(threshold int) *FaucetVoteCollector {
	return &FaucetVoteCollector{
		voters:    make(map[string]map[peer.ID]bool),
		threshold: threshold,
	}
}

func (c *FaucetVoteCollector) ProcessVote(txHash string, voterID peer.ID, approve bool) {
	c.AddVote(txHash, voterID, approve)
}

func (c *FaucetVoteCollector) AddVote(txHash string, voterID peer.ID, approve bool) {
	if _, exists := c.voters[txHash]; !exists {
		c.voters[txHash] = make(map[peer.ID]bool)
	}
	c.voters[txHash][voterID] = approve
}
func (c *FaucetVoteCollector) HasReachedThreshold(txHash string) bool {
	approvals := 0
	if voters, exists := c.voters[txHash]; exists {
		for _, approve := range voters {
			if approve {
				approvals++
			}
		}
	}
	return approvals >= c.threshold
}

func (c *FaucetVoteCollector) HasReachedRejectThreshold(txHash string) bool {
	rejects := 0
	if voters, exists := c.voters[txHash]; exists {
		for _, approve := range voters {
			if !approve {
				rejects++
			}
		}
	}
	return rejects >= c.threshold
}

func (c *FaucetVoteCollector) ClearVotes(txHash string) {
	delete(c.voters, txHash)
}

func (c *FaucetVoteCollector) GetVotes() map[string]map[peer.ID]bool {
	return c.voters
}

func (c *FaucetVoteCollector) setVotes(votes map[string]map[peer.ID]bool) {
	c.voters = votes
}
