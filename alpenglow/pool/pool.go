package pool

import (
	"errors"

	"github.com/mezonai/mmn/alpenglow/votor"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/utils"
)

type Pool struct {
	slotState          map[uint64]*SlotState
	parentReadyTracker *ParentReadyTracker
	finalityTracker    *FinalityTracker
	votorChannel       chan votor.VotorEvent
	ownPubKey          string
}

func NewPool(parentReadyTracker *ParentReadyTracker, finalityTracker *FinalityTracker, votorChannel chan votor.VotorEvent, ownPubKey string) *Pool {
	return &Pool{
		slotState:          make(map[uint64]*SlotState),
		parentReadyTracker: parentReadyTracker,
		finalityTracker:    finalityTracker,
		votorChannel:       votorChannel,
		ownPubKey:          ownPubKey,
	}
}

func (p *Pool) getSlotState(slot uint64) *SlotState {
	if state, exists := p.slotState[slot]; exists {
		return state
	}
	p.slotState[slot] = NewSlotState(slot, p.ownPubKey)
	return p.slotState[slot]
}

func (p *Pool) AddVote(v *consensus.Vote) (bool, error) {
	latestFinalizedSlot := p.finalityTracker.GetHighestFinalizedSlot()
	if v.Slot <= latestFinalizedSlot || v.Slot >= latestFinalizedSlot+2*utils.SLOTS_PER_EPOCH {
		return false, errors.New("slot out of range")
	}

	if valid, err := p.getSlotState(v.Slot).CheckSlashableOffense(v); !valid {
		return false, err
	} else if p.getSlotState(v.Slot).ContainsVote(v) {
		return false, errors.New("duplicate vote")
	}

	// Add vote to slot state
	newCerts, newVotorEvents := p.getSlotState(v.Slot).AddVote(v)

	// If new certs created => add them to pool
	for _, cert := range newCerts {
		p.addValidCert(&cert)
	}

	// If new votor events created => send them to votor
	for _, event := range newVotorEvents {
		p.votorChannel <- event
	}

	//TODO: handle repair events if needed

	return true, nil
}

func (p *Pool) AddCert(c *consensus.Cert) (bool, error) {
	latestFinalizedSlot := p.finalityTracker.GetHighestFinalizedSlot()
	if c.Slot <= latestFinalizedSlot || c.Slot > latestFinalizedSlot+utils.SLOTS_PER_EPOCH {
		return false, errors.New("slot out of range")
	}

	if valid := c.VerifySignature(); !valid {
		return false, errors.New("invalid certificate signature")
	}

	if p.getSlotState(c.Slot).ContainsCert(c) {
		return false, errors.New("duplicate certificate")
	}

	p.addValidCert(c)

	return true, nil
}

func (p *Pool) addValidCert(cert *consensus.Cert) {
	// Add cert to slot state
	p.getSlotState(cert.Slot).AddCert(cert)

	switch cert.CertType {
	case consensus.NOTAR_CERT, consensus.NOTAR_FALLBACK_CERT:
		if cert.CertType == consensus.NOTAR_CERT {
			finalizationEvent := p.finalityTracker.MarkNotarized(cert.Slot, cert.BlockHash)
			p.handleFinalization(finalizationEvent)
		}

		//TODO: if have child block waiting for this parent => notify to them (s2n_waiting_parent_cert - Solana)

		newParentsReady := p.parentReadyTracker.MarkNotarFallback(BlockId{Slot: cert.Slot, BlockHash: cert.BlockHash})
		p.sentParentReadyEvents(newParentsReady)

		//TODO: handle repair events if needed

	case consensus.SKIP_CERT:
		newParentReady := p.parentReadyTracker.MarkSkipped(cert.Slot)
		p.sentParentReadyEvents(newParentReady)

	case consensus.FAST_FINAL_CERT:
		finalizationEvent := p.finalityTracker.MarkFastFinalized(cert.Slot, cert.BlockHash)
		p.handleFinalization(finalizationEvent)
		p.prune()

	case consensus.FINAL_CERT:
		finalizationEvent := p.finalityTracker.MarkFinalized(cert.Slot)
		p.handleFinalization(finalizationEvent)
		p.prune()

	}

	p.votorChannel <- votor.VotorEvent{
		Type: votor.CERT_CREATED,
		Cert: cert,
	}
}

func (p *Pool) handleFinalization(event FinalizationEvent) {
	newParentsReady := p.parentReadyTracker.HandleFinalization(event)
	p.sentParentReadyEvents(newParentsReady)

}

func (p *Pool) sentParentReadyEvents(newParentsReadys []SlotBlockId) {
	for _, parentReady := range newParentsReadys {
		p.votorChannel <- votor.VotorEvent{
			Type: votor.PARENT_READY,
			Slot: parentReady.Slot,
			Block: votor.BlockInfo{
				Slot:       parentReady.Slot,
				ParentSlot: parentReady.BlockId.Slot,
				ParentHash: parentReady.BlockId.BlockHash,
			},
		}
	}
}

func (p *Pool) prune() {
	lastSlot := p.finalityTracker.GetHighestFinalizedSlot()
	newSlotState := make(map[uint64]*SlotState)
	for slot, state := range p.slotState {
		if slot >= lastSlot {
			newSlotState[slot] = state
		}
	}
	p.slotState = newSlotState
}
