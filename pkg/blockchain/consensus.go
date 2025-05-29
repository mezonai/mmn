// File: pkg/blockchain/consensus.go
package blockchain

import (
	"fmt"
	"sync"
)

// CandidateBlock represents a proposed block with associated work and votes.
type CandidateBlock struct {
	Block      *Block
	Work       int // For example, the nonce value (as a proxy for work)
	ValidVotes int // Sum of votes (weighted by stake)
}

// HybridConsensusManager handles candidate block proposals and validator votes.
type HybridConsensusManager struct {
	CandidateBlocks []*CandidateBlock
	Stakeholders    map[string]float64 // e.g., {"Miner1":50.0, "Validator1":30.0, ...}
	VoteThreshold   float64            // e.g., 0.67 (67% of total stake)
	mu              sync.Mutex
}

// NewHybridConsensusManager creates a new consensus manager.
func NewHybridConsensusManager() *HybridConsensusManager {
	return &HybridConsensusManager{
		CandidateBlocks: []*CandidateBlock{},
		Stakeholders:    make(map[string]float64),
		VoteThreshold:   0.67,
	}
}

// ProposeBlock adds a new candidate block after PoW.
func (hcm *HybridConsensusManager) ProposeBlock(b *Block) {
	hcm.mu.Lock()
	defer hcm.mu.Unlock()
	candidate := &CandidateBlock{
		Block:      b,
		Work:       b.Nonce,
		ValidVotes: 0,
	}
	hcm.CandidateBlocks = append(hcm.CandidateBlocks, candidate)
	fmt.Printf("Block proposed: %s with work %d\n", b.Hash, b.Nonce)
}

// CastVote adds a vote (true for approval) from a validator.
func (hcm *HybridConsensusManager) CastVote(candidateIndex int, validator string, vote bool) {
	hcm.mu.Lock()
	defer hcm.mu.Unlock()
	if candidateIndex < 0 || candidateIndex >= len(hcm.CandidateBlocks) {
		fmt.Println("Invalid candidate index")
		return
	}
	if vote {
		stake, exists := hcm.Stakeholders[validator]
		if !exists {
			fmt.Printf("Validator %s not found\n", validator)
			return
		}
		hcm.CandidateBlocks[candidateIndex].ValidVotes += int(stake * 100) // Scale stake for demo.
	}
}

// FinalizeBlock returns a candidate block if it meets the threshold.
func (hcm *HybridConsensusManager) FinalizeBlock(totalStake int) *Block {
	hcm.mu.Lock()
	defer hcm.mu.Unlock()
	threshold := int(float64(totalStake) * hcm.VoteThreshold)
	for _, candidate := range hcm.CandidateBlocks {
		if candidate.ValidVotes >= threshold {
			fmt.Printf("Finalizing block %s with votes %d (threshold %d)\n", candidate.Block.Hash, candidate.ValidVotes, threshold)
			return candidate.Block
		}
	}
	return nil
}
