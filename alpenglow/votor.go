package alpenglow

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/mezonai/mmn/alpenglow/pool"
	"github.com/mezonai/mmn/alpenglow/votor"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/p2p"
	"github.com/mezonai/mmn/utils"
)

const (
	DELTA_BLOCK   = 400 * time.Millisecond   //400ms for each block
	DELTA_TIMEOUT = (25 * DELTA_BLOCK) / 100 //25% DELTA_BLOCK for timeout network propagation
)

type BroadcastedCert struct {
	notar         bool
	notarFallback map[[32]byte]struct{}
	skip          bool
	final         bool
	finalFallback bool
}

type Votor struct {
	voted           map[uint64]struct{}
	votedNotar      map[uint64][32]byte
	badWindow       map[uint64]struct{}
	blockNotarized  map[uint64][32]byte
	parentsReady    map[votor.ParentReadyKey]struct{}
	pendingBlocks   map[uint64]votor.BlockInfo
	broadcastedCert map[uint64]*BroadcastedCert
	retiredSlots    map[uint64]struct{}
	blsPubKey       string
	blsPrivKey      bls.SecretKey
	eventReceiver   <-chan votor.VotorEvent
	eventSender     chan<- votor.VotorEvent
	ln              *p2p.Libp2pNetwork
	pool            *pool.Pool
	quit            chan struct{}
}

func NewVotor(blsPubKey string, blsPrivKey bls.SecretKey, eventReceiver <-chan votor.VotorEvent, eventSender chan<- votor.VotorEvent, ln *p2p.Libp2pNetwork, pool *pool.Pool) *Votor {
	return &Votor{
		voted:           make(map[uint64]struct{}),
		votedNotar:      make(map[uint64][32]byte),
		badWindow:       make(map[uint64]struct{}),
		blockNotarized:  make(map[uint64][32]byte),
		parentsReady:    make(map[votor.ParentReadyKey]struct{}),
		pendingBlocks:   make(map[uint64]votor.BlockInfo),
		broadcastedCert: make(map[uint64]*BroadcastedCert),
		retiredSlots:    make(map[uint64]struct{}),
		blsPubKey:       blsPubKey,
		blsPrivKey:      blsPrivKey,
		eventReceiver:   eventReceiver,
		eventSender:     eventSender,
		ln:              ln,
		pool:            pool,
		quit:            make(chan struct{}),
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

func (v *Votor) getBroadcastedCert(slot uint64) *BroadcastedCert {
	broadcastedCert, exists := v.broadcastedCert[slot]
	if !exists {
		v.broadcastedCert[slot] = &BroadcastedCert{
			notar:         false,
			notarFallback: make(map[[32]byte]struct{}),
			skip:          false,
			final:         false,
			finalFallback: false,
		}
		return v.broadcastedCert[slot]
	}
	return broadcastedCert
}

func (v *Votor) handleEvent(ev votor.VotorEvent) {
	switch ev.Type {
	case votor.BLOCK_RECEIVED:
		logx.Info("VOTOR", fmt.Sprintf("Received block %s in slot %d", hex.EncodeToString(ev.BlockHash[:]), ev.Slot))
		if _, exists := v.voted[ev.Slot]; exists {
			logx.Info("VOTOR", fmt.Sprintf("Not voting for block %s in slot %d, already voted", hex.EncodeToString(ev.BlockHash[:]), ev.Slot))
			return
		}

		if success := v.tryNotar(ev.Block); success {
		} else {
			logx.Info("VOTOR", fmt.Sprintf("Cannot vote for block %s in slot %d, missing parent or parent not ready", hex.EncodeToString(ev.BlockHash[:]), ev.Slot))
			v.pendingBlocks[ev.Slot] = ev.Block
		}

	case votor.PARENT_READY:
		logx.Info("VOTOR", fmt.Sprintf("Parent ready for slot %d, parent slot %d, parent hash %s", ev.Slot, ev.Block.ParentSlot, hex.EncodeToString(ev.Block.ParentHash[:])))
		v.handleParentReady(ev)

	case votor.TIMEOUT:
		if _, voted := v.voted[ev.Slot]; !voted {
			logx.Info("VOTOR", fmt.Sprintf("Timeout for slot %d, trying to skip window", ev.Slot))
			v.trySkipWindow(ev.Slot)
		}

	case votor.SAFE_TO_NOTAR:
		logx.Info("VOTOR", fmt.Sprintf("Safe to notar for slot %d, block hash %s", ev.Slot, hex.EncodeToString(ev.BlockHash[:])))
		v.handleSafeToNotar(ev)

	case votor.SAFE_TO_SKIP:
		logx.Info("VOTOR", fmt.Sprintf("Safe to skip for slot %d", ev.Slot))
		v.handleSafeToSkip(ev)

	case votor.CERT_CREATED:
		logx.Info("VOTOR", fmt.Sprintf("Cert created for slot %d, type %v, block hash %s", ev.Cert.Slot, ev.Cert.CertType, hex.EncodeToString(ev.Cert.BlockHash[:])))
		v.handleCertCreated(ev)

	case votor.CERT_SAVED:
		logx.Info("VOTOR", fmt.Sprintf("Cert saved for slot %d, type %v, block hash %s", ev.Cert.Slot, ev.Cert.CertType, hex.EncodeToString(ev.Cert.BlockHash[:])))
		v.handleCertSaved(ev)

	}

}

func (v *Votor) tryNotar(blockInfo votor.BlockInfo) bool {
	slot := blockInfo.Slot
	hash := blockInfo.Hash
	parentSlot := blockInfo.ParentSlot
	parentHash := blockInfo.ParentHash

	logx.Info("VOTOR", fmt.Sprintf("Trying notar for block %s in slot %d", hex.EncodeToString(hash[:]), slot))

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
	} else if parentSlot >= slot || (v.votedNotar[parentSlot] != parentHash && parentSlot != 0) {
		return false
	}

	vote := &consensus.Vote{
		Slot:      slot,
		VoteType:  consensus.NOTAR_VOTE,
		BlockHash: hash,
		PubKey:    v.blsPubKey,
	}
	vote.Sign(v.blsPrivKey)
	v.addVoteToPoolAndBroadcast(vote)
	v.voted[slot] = struct{}{}
	v.votedNotar[slot] = hash
	delete(v.pendingBlocks, slot)
	v.tryFinal(slot, hash)
	logx.Info("VOTOR", fmt.Sprintf("Voted notar for block %s in slot %d", hex.EncodeToString(hash[:]), slot))
	return true
}

func (v *Votor) tryFinal(slot uint64, hash [32]byte) {
	logx.Info("VOTOR", fmt.Sprintf("Trying final for block %s in slot %d", hex.EncodeToString(hash[:]), slot))
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
		v.addVoteToPoolAndBroadcast(vote)
		v.retiredSlots[slot] = struct{}{}
		logx.Info("VOTOR", fmt.Sprintf("Voted final for block %s in slot %d", hex.EncodeToString(hash[:]), slot))
	}
}

func (v *Votor) trySkipWindow(currentSlot uint64) {
	logx.Info("VOTOR", fmt.Sprintf("Trying skip window at slot %d", currentSlot))
	for _, slot := range utils.SlotsInWindow(currentSlot) {
		if _, voted := v.voted[slot]; !voted {
			v.badWindow[slot] = struct{}{}
			vote := &consensus.Vote{
				Slot:     slot,
				VoteType: consensus.SKIP_VOTE,
				PubKey:   v.blsPubKey,
			}
			vote.Sign(v.blsPrivKey)
			v.addVoteToPoolAndBroadcast(vote)
			v.voted[slot] = struct{}{}
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
	broadcastedCert := v.getBroadcastedCert(cert.Slot)

	switch cert.CertType {
	case consensus.NOTAR_CERT:
		if broadcastedCert.notar {
			return
		}
		broadcastedCert.notar = true

	case consensus.NOTAR_FALLBACK_CERT:
		if _, exists := broadcastedCert.notarFallback[cert.BlockHash]; exists {
			return
		}
		broadcastedCert.notarFallback[cert.BlockHash] = struct{}{}

	case consensus.SKIP_CERT:
		if broadcastedCert.skip {
			return
		}
		broadcastedCert.skip = true

	case consensus.FINAL_CERT:
		if broadcastedCert.final {
			return
		}
		broadcastedCert.final = true

	case consensus.FAST_FINAL_CERT:
		if broadcastedCert.finalFallback {
			return
		}
		broadcastedCert.finalFallback = true

	}

	v.ln.BroadcastCert(context.Background(), cert)
}

func (v *Votor) handleCertSaved(ev votor.VotorEvent) {
	cert := ev.Cert

	switch cert.CertType {
	case consensus.NOTAR_CERT:
		slot := cert.Slot
		blockHash := cert.BlockHash
		v.blockNotarized[slot] = blockHash
		v.tryFinal(slot, blockHash)

	case consensus.FINAL_CERT, consensus.FAST_FINAL_CERT:
		delete(v.broadcastedCert, cert.Slot)
		// Fallback: set timeouts on Final/FastFinal to recover if ParentReady was missed.
		firstSlotInWindow := utils.FirstSlotInWindow(cert.Slot)
		v.setTimeouts(firstSlotInWindow)
	}
}

func (v *Votor) handleSafeToNotar(ev votor.VotorEvent) {
	vote := &consensus.Vote{
		Slot:      ev.Slot,
		VoteType:  consensus.NOTAR_FALLBACK_VOTE,
		BlockHash: ev.BlockHash,
		PubKey:    v.blsPubKey,
	}
	vote.Sign(v.blsPrivKey)
	v.addVoteToPoolAndBroadcast(vote)
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
	v.addVoteToPoolAndBroadcast(vote)
	v.trySkipWindow(ev.Slot)
	v.badWindow[ev.Slot] = struct{}{}
}

func (v *Votor) addVoteToPoolAndBroadcast(vote *consensus.Vote) {
	// First save vote to pool
	v.pool.AddVote(vote)
	// Then broadcast to network
	v.ln.BroadcastVote(context.Background(), vote)
}
