package pool

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/mezonai/mmn/alpenglow/votor"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/utils"
)

type Pool struct {
	slotState            map[uint64]*SlotState
	parentReadyTracker   *ParentReadyTracker
	finalityTracker      *FinalityTracker
	s2nWaitingParentCert map[BlockId]BlockId // Safe to notar cert waiting for parent cert
	votorChannel         chan votor.VotorEvent
	repairChannel        chan BlockId
	ownPubKey            string
}

func NewPool(parentReadyTracker *ParentReadyTracker, finalityTracker *FinalityTracker, votorChannel chan votor.VotorEvent, repairChannel chan BlockId, ownPubKey string) *Pool {
	return &Pool{
		slotState:            make(map[uint64]*SlotState),
		parentReadyTracker:   parentReadyTracker,
		finalityTracker:      finalityTracker,
		s2nWaitingParentCert: make(map[BlockId]BlockId),
		votorChannel:         votorChannel,
		repairChannel:        repairChannel,
		ownPubKey:            ownPubKey,
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
	logx.Info("POOL", fmt.Sprintf("Received vote of type %v for block %s in slot %d from %s", v.VoteType, hex.EncodeToString(v.BlockHash[:]), v.Slot, v.PubKey))

	latestFinalizedSlot := p.finalityTracker.GetHighestFinalizedSlot()
	if v.Slot <= latestFinalizedSlot || v.Slot >= latestFinalizedSlot+(2*utils.SLOTS_PER_WINDOW) {
		return false, errors.New("slot out of range")
	}

	if valid, err := p.getSlotState(v.Slot).CheckSlashableOffense(v); !valid {
		return false, err
	} else if p.getSlotState(v.Slot).ContainsVote(v) {
		return false, errors.New("duplicate vote")
	}

	// Add vote to slot state
	logx.Info("POOL", fmt.Sprintf("Adding %v vote for block %s in slot %d", v.VoteType, hex.EncodeToString(v.BlockHash[:]), v.Slot))
	newCerts, newVotorEvents, newRepairBlocks := p.getSlotState(v.Slot).AddVote(v)

	// If new certs created => send events to votor
	for _, cert := range newCerts {
		p.votorChannel <- votor.VotorEvent{
			Type: votor.CERT_CREATED,
			Cert: &cert,
		}
	}

	// If new votor events created => send them to votor
	for _, event := range newVotorEvents {
		p.votorChannel <- event
	}

	// If new repair blocks created => send them to repair worker
	for _, blockId := range newRepairBlocks {
		p.repairChannel <- blockId
	}

	return true, nil
}

func (p *Pool) AddCert(c *consensus.Cert) (bool, error) {
	logx.Info("POOL", fmt.Sprintf("Received certificate of type %v for block %s in slot %d", c.CertType, hex.EncodeToString(c.BlockHash[:]), c.Slot))

	latestFinalizedSlot := p.finalityTracker.GetHighestFinalizedSlot()
	if c.Slot <= latestFinalizedSlot || c.Slot >= latestFinalizedSlot+(2*utils.SLOTS_PER_WINDOW) {
		return false, errors.New("slot out of range")
	}

	if p.getSlotState(c.Slot).ContainsCert(c) {
		return false, errors.New("duplicate certificate")
	}

	p.addValidCert(c)

	return true, nil
}

func (p *Pool) addValidCert(cert *consensus.Cert) {
	slot := cert.Slot
	blockHash := cert.BlockHash

	// Add cert to slot state
	logx.Info("POOL", fmt.Sprintf("Adding %v certificate for block %s in slot %d", cert.CertType, hex.EncodeToString(cert.BlockHash[:]), cert.Slot))
	p.getSlotState(slot).AddCert(cert)

	switch cert.CertType {
	case consensus.NOTAR_CERT, consensus.NOTAR_FALLBACK_CERT:
		if cert.CertType == consensus.NOTAR_CERT {
			finalizationEvent := p.finalityTracker.MarkNotarized(slot, blockHash)
			p.handleFinalization(finalizationEvent)
		}

		if childId, exists := p.s2nWaitingParentCert[BlockId{Slot: slot, BlockHash: blockHash}]; exists {
			event, parentBlockId := p.getSlotState(childId.Slot).NotifyParentCertified(childId.BlockHash)
			delete(p.s2nWaitingParentCert, BlockId{Slot: slot, BlockHash: blockHash})

			if event != nil {
				p.votorChannel <- *event
			}
			if parentBlockId != nil {
				p.repairChannel <- *parentBlockId
			}
		}

		newParentsReady := p.parentReadyTracker.MarkNotarFallback(BlockId{Slot: slot, BlockHash: blockHash})
		p.sentParentReadyEvents(newParentsReady)

		p.repairChannel <- BlockId{Slot: slot, BlockHash: blockHash}

	case consensus.SKIP_CERT:
		newParentReady := p.parentReadyTracker.MarkSkipped(slot)
		p.sentParentReadyEvents(newParentReady)

	case consensus.FAST_FINAL_CERT:
		finalizationEvent := p.finalityTracker.MarkFastFinalized(slot, blockHash)
		p.handleFinalization(finalizationEvent)
		p.prune()

	case consensus.FINAL_CERT:
		finalizationEvent := p.finalityTracker.MarkFinalized(slot)
		p.handleFinalization(finalizationEvent)
		p.prune()

	}

	p.votorChannel <- votor.VotorEvent{
		Type: votor.CERT_SAVED,
		Cert: cert,
	}
}

func (p *Pool) handleFinalization(event FinalizationEvent) {
	newParentsReady := p.parentReadyTracker.HandleFinalization(event)
	p.sentParentReadyEvents(newParentsReady)
	p.finalityTracker.Prune()
	p.parentReadyTracker.Prune(p.finalityTracker.GetHighestFinalizedSlot())
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

func (p *Pool) AddBlock(blockId BlockId, parentId BlockId) {
	if blockId.Slot <= parentId.Slot {
		panic("block slot must be greater than parent slot")
	}

	finalizationEvent := p.finalityTracker.AddParent(blockId, parentId)
	newParentReady := p.parentReadyTracker.HandleFinalization(finalizationEvent)
	p.sentParentReadyEvents(newParentReady)

	logx.Info("POOL", fmt.Sprintf("Notify slot %d that parent block %s is known", blockId.Slot, hex.EncodeToString(parentId.BlockHash[:])))
	p.getSlotState(blockId.Slot).NotifyParentKnown(blockId.BlockHash)

	parentState := p.getSlotState(parentId.Slot)

	if parentState.isNotarFallback(parentId.BlockHash) {
		event, parentBlockId := p.getSlotState(blockId.Slot).NotifyParentCertified(blockId.BlockHash)
		logx.Info("POOL", fmt.Sprintf("Notify slot %d that parent block %s is certified", blockId.Slot, hex.EncodeToString(parentId.BlockHash[:])))

		if event != nil {
			p.votorChannel <- *event
		}
		if parentBlockId != nil {
			p.repairChannel <- *parentBlockId

		}

		return
	}

	p.s2nWaitingParentCert[parentId] = blockId
}

func (p *Pool) GetParentsReady(slot uint64) BlockId {
	return p.parentReadyTracker.GetParentsReady(slot)
}

func (p *Pool) IsSlotSkipped(slot uint64) bool {
	return p.parentReadyTracker.getState(slot).Skip
}

func (p *Pool) GetHighestFinalizedSlot() uint64 {
	return p.finalityTracker.GetHighestFinalizedSlot()
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
