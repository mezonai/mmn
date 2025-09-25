package alpenglow

import (
	"context"
	"fmt"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/mezonai/mmn/alpenglow/votor"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/p2p"
	"github.com/mezonai/mmn/utils"
)

const (
	DELTA_BLOCK   = 4 * time.Millisecond   //400ms for each block
	DELTA_TIMEOUT = 200 * time.Millisecond //200ms for timeout network propagation
)

type Votor struct {
	voted          map[uint64]struct{}
	votedNotar     map[uint64][32]byte
	badWindow      map[uint64]struct{}
	blockNotarized map[uint64][32]byte
	parentsReady   map[votor.ParentReadyKey]struct{}
	pendingBlocks  map[uint64]votor.BlockInfo
	retiredSlots   map[uint64]struct{}
	blsPubKey      string
	blsPrivKey     bls.SecretKey
	eventReceiver  <-chan votor.VotorEvent
	eventSender    chan<- votor.VotorEvent
	ln             *p2p.Libp2pNetwork
	quit           chan struct{}
}

func NewVotor(blsPubKey string, blsPrivKey bls.SecretKey, eventReceiver <-chan votor.VotorEvent, eventSender chan<- votor.VotorEvent, ln *p2p.Libp2pNetwork) *Votor {
	return &Votor{
		voted:          make(map[uint64]struct{}),
		votedNotar:     make(map[uint64][32]byte),
		badWindow:      make(map[uint64]struct{}),
		blockNotarized: make(map[uint64][32]byte),
		parentsReady:   make(map[votor.ParentReadyKey]struct{}),
		pendingBlocks:  make(map[uint64]votor.BlockInfo),
		retiredSlots:   make(map[uint64]struct{}),
		blsPubKey:      blsPubKey,
		blsPrivKey:     blsPrivKey,
		eventReceiver:  eventReceiver,
		eventSender:    eventSender,
		ln:             ln,
		quit:           make(chan struct{}),
	}
}

func (v *Votor) Run() {
	for {
		select {
		case ev := <-v.eventReceiver:
			v.handleEvent(ev)
		case <-v.quit:
			return
		}
	}
}

func (v *Votor) handleEvent(ev votor.VotorEvent) {
	switch ev.Type {
	case votor.BLOCK_RECEIVED:
		if _, exists := v.voted[ev.Slot]; exists {
			fmt.Printf("not voting for block %s in slot %d, already voted\n", ev.BlockHash, ev.Slot)
			return
		}

		if success := v.tryNotar(ev.Block); success {
			fmt.Printf("voted for block %s in slot %d\n", ev.BlockHash, ev.Slot)
		} else {
			v.pendingBlocks[ev.Slot] = ev.Block
		}

	case votor.PARENT_READY:
		v.handleParentReady(ev)

	case votor.TIMEOUT:
		if _, voted := v.voted[ev.Slot]; !voted {
			v.trySkipWindow(ev.Slot)
			fmt.Printf("timeout for slot %d, marked bad window\n", ev.Slot)
		}

	case votor.SAFE_TO_NOTAR:
		v.handleSafeToNotar(ev)

	case votor.SAFE_TO_SKIP:
		v.handleSafeToSkip(ev)

	case votor.CERT_CREATED:
		v.handleCertCreated(ev)
	}
}

func (v *Votor) tryNotar(blockInfo votor.BlockInfo) bool {
	slot := blockInfo.Slot
	hash := blockInfo.Hash
	parentSlot := blockInfo.ParentSlot
	parentHash := blockInfo.ParentHash

	first_slot_in_window := utils.FirstSlotInWindow(slot)
	if slot == first_slot_in_window && parentSlot != 0 {
		parentReadyKey := votor.ParentReadyKey{
			Slot:       slot,
			ParentSlot: parentSlot,
			ParentHash: parentHash,
		}
		if _, exists := v.parentsReady[parentReadyKey]; !exists {
			return false
		}
	} else if parentSlot >= slot || v.votedNotar[parentSlot] != parentHash {
		return false
	}

	vote := &consensus.Vote{
		Slot:      slot,
		VoteType:  consensus.NOTAR_VOTE,
		BlockHash: hash,
		PubKey:    v.blsPubKey,
	}
	vote.Sign(v.blsPrivKey)
	v.ln.BroadcastVote(context.Background(), vote)
	v.voted[slot] = struct{}{}
	v.votedNotar[slot] = hash
	delete(v.pendingBlocks, slot)
	v.tryFinal(slot, hash)
	return true
}

func (v *Votor) tryFinal(slot uint64, hash [32]byte) {
	isNotarized := false
	if blockHash, exists := v.blockNotarized[slot]; exists && blockHash == hash {
		isNotarized = true
	}
	isVotedNotar := false
	if blockHash, exists := v.votedNotar[slot]; exists && blockHash == hash {
		isVotedNotar = true
	}
	_, isBadWindow := v.badWindow[slot]

	if isNotarized && isVotedNotar && !isBadWindow {
		vote := &consensus.Vote{
			Slot:      slot,
			VoteType:  consensus.FINAL_VOTE,
			BlockHash: hash,
			PubKey:    v.blsPubKey,
		}
		vote.Sign(v.blsPrivKey)
		v.ln.BroadcastVote(context.Background(), vote)
		v.retiredSlots[slot] = struct{}{}
	}
}

func (v *Votor) trySkipWindow(currentSlot uint64) {
	for _, slot := range utils.SlotsInWindow(currentSlot) {
		if _, voted := v.voted[slot]; !voted {
			v.badWindow[slot] = struct{}{}
			vote := &consensus.Vote{
				Slot:     slot,
				VoteType: consensus.SKIP_VOTE,
				PubKey:   v.blsPubKey,
			}
			vote.Sign(v.blsPrivKey)
			v.ln.BroadcastVote(context.Background(), vote)
			fmt.Printf("skipping slot %d, marked bad window\n", slot)
		}
	}
}

func (v *Votor) setTimeouts(currentSlot uint64) {
	if !utils.IsSlotStartOfWindow(currentSlot) {
		panic("setTimeouts called on non-window-start slot")
	}

	sender := v.eventSender
	slots := utils.SlotsInWindow(currentSlot)

	// TODO: set timeouts only once? (Sonlana)
	// Set timeout for each slot in the window
	go func() {
		for _, s := range slots {
			if utils.IsSlotStartOfWindow(s) {
				time.Sleep(DELTA_TIMEOUT + DELTA_BLOCK)
				event := votor.VotorEvent{
					Type: votor.TIMEOUT,
					Slot: s,
				}
				sender <- event
			} else {
				time.Sleep(DELTA_BLOCK)
				event := votor.VotorEvent{
					Type: votor.TIMEOUT,
					Slot: s,
				}
				sender <- event
			}
		}
	}()

}

func (v *Votor) checkPendingBlocks() {
	for _, blockInfo := range v.pendingBlocks {
		v.tryNotar(blockInfo)
	}
}

func (v *Votor) handleParentReady(ev votor.VotorEvent) {
	v.parentsReady[votor.ParentReadyKey{
		Slot:       ev.Slot,
		ParentSlot: ev.Block.ParentSlot,
		ParentHash: ev.Block.ParentHash,
	}] = struct{}{}

	v.checkPendingBlocks()
	v.setTimeouts(ev.Slot)
}

func (v *Votor) handleCertCreated(ev votor.VotorEvent) {
	cert := ev.Cert

	switch cert.CertType {
	case consensus.NOTAR_CERT:
		slot := cert.Slot
		blockHash := cert.BlockHash
		v.blockNotarized[slot] = blockHash
		v.tryFinal(slot, blockHash)

	case consensus.FINAL_CERT, consensus.FAST_FINAL_CERT:
		// Fallback: set timeouts on Final/FastFinal to recover if ParentReady was missed.
		firstSlotInWindow := utils.FirstSlotInWindow(cert.Slot)
		v.setTimeouts(firstSlotInWindow)
	}

	// Broadcast the cert to the network
	v.ln.BroadcastCert(context.Background(), cert)
}

func (v *Votor) handleSafeToNotar(ev votor.VotorEvent) {
	vote := &consensus.Vote{
		Slot:      ev.Slot,
		VoteType:  consensus.NOTAR_FALLBACK_VOTE,
		BlockHash: ev.BlockHash,
		PubKey:    v.blsPubKey,
	}
	vote.Sign(v.blsPrivKey)
	v.ln.BroadcastVote(context.Background(), vote)
	v.trySkipWindow(ev.Slot)
	v.badWindow[ev.Slot] = struct{}{}
}

func (v *Votor) handleSafeToSkip(ev votor.VotorEvent) {
	vote := &consensus.Vote{
		Slot:     ev.Slot,
		VoteType: consensus.SKIP_FALLBACK_VOTE,
		PubKey:   v.blsPubKey,
	}
	vote.Sign(v.blsPrivKey)
	v.ln.BroadcastVote(context.Background(), vote)
	v.trySkipWindow(ev.Slot)
	v.badWindow[ev.Slot] = struct{}{}
}
