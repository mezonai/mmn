package pool

import (
	"errors"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/mezonai/mmn/alpenglow/votor"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/utils"
)

const (
	TOTAL_NODE = 3 //TODO: not hardcoded, read form config
)

type SafeToNotarStatus int

const (
	SAFE_TO_NOTAR SafeToNotarStatus = iota
	MISSING_BLOCK
	AWAITING_VOTES
)

type ParentStatus int

const (
	KNOWN ParentStatus = iota
	CERTIFIED
)

type HashVotes struct {
	hash [32]byte
	vote *consensus.Vote
}

// Stores all vote from network for a slot.
type SlotVotes struct {
	notar         map[string]*HashVotes // voterID -> HashVotes
	notarFallback map[string][]*HashVotes
	skip          map[string]*consensus.Vote
	skipFallback  map[string]*consensus.Vote
	final         map[string]*consensus.Vote
}

// Calculates stakes for each block hash in a slot.
type SlotVotedStakes struct {
	notar         map[[32]byte]uint64 // hash -> stake(sum of voters)
	notarFallback map[[32]byte]uint64
	skip          uint64
	skipFallback  uint64
	final         uint64
	notarOrSkip   uint64 // Amount of stake that voted notar or skip
	topNotar      uint64 // maximum amount of stake for a single hash
}

type SlotCertificates struct {
	notar         *consensus.Cert
	notarFallback []*consensus.Cert
	skip          *consensus.Cert
	fastFinalize  *consensus.Cert
	finalize      *consensus.Cert
}

type SlotState struct {
	slot               uint64
	votes              SlotVotes
	votedStakes        SlotVotedStakes
	certificates       SlotCertificates
	parents            map[[32]byte]ParentStatus // child hash -> ParentStatus
	pendingSafeToNotar map[[32]byte]struct{}
	sentSafeToNotar    map[[32]byte]struct{}
	sentSafeToSkip     bool
	ownPubKey          string
}

func NewSlotState(slot uint64, ownPubKey string) *SlotState {
	return &SlotState{
		slot: slot,
		votes: SlotVotes{
			notar:         make(map[string]*HashVotes),
			notarFallback: make(map[string][]*HashVotes),
			skip:          make(map[string]*consensus.Vote),
			skipFallback:  make(map[string]*consensus.Vote),
			final:         make(map[string]*consensus.Vote),
		},
		votedStakes: SlotVotedStakes{
			notar:         make(map[[32]byte]uint64),
			notarFallback: make(map[[32]byte]uint64),
			skip:          0,
			skipFallback:  0,
			final:         0,
			notarOrSkip:   0,
			topNotar:      0,
		},
		certificates:       SlotCertificates{},
		parents:            make(map[[32]byte]ParentStatus),
		pendingSafeToNotar: make(map[[32]byte]struct{}),
		sentSafeToNotar:    make(map[[32]byte]struct{}),
		sentSafeToSkip:     false,
		ownPubKey:          ownPubKey,
	}
}

func (ss *SlotState) CheckSlashableOffense(v *consensus.Vote) (bool, error) {
	voter := v.PubKey

	switch v.VoteType {
	case consensus.NOTAR_VOTE:
		blockHash := v.BlockHash
		if _, exists := ss.votes.skip[voter]; exists {
			return false, errors.New("slashable offense: voted skip and notar")
		}
		if hv, exists := ss.votes.notar[voter]; exists && hv.hash != blockHash {
			return false, errors.New("slashable offense: voted different block")
		}

	case consensus.NOTAR_FALLBACK_VOTE:
		if _, exists := ss.votes.final[voter]; exists {
			return false, errors.New("slashable offense: voted notar fallback and final")
		}

	case consensus.SKIP_VOTE:
		if _, exists := ss.votes.final[voter]; exists {
			return false, errors.New("slashable offense: voted skip and final")
		}
		if _, exists := ss.votes.notar[voter]; exists {
			return false, errors.New("slashable offense: voted skip and notar")
		}

	case consensus.SKIP_FALLBACK_VOTE:
		if _, exists := ss.votes.final[voter]; exists {
			return false, errors.New("slashable offense: voted skip fallback and final")
		}

	case consensus.FINAL_VOTE:
		if _, exists := ss.votes.skip[voter]; exists {
			return false, errors.New("slashable offense: voted final and skip")
		}
		if _, exists := ss.votes.skipFallback[voter]; exists {
			return false, errors.New("slashable offense: voted final and skip fallback")
		}
		if _, exists := ss.votes.notar[voter]; !exists {
			return false, errors.New("slashable offense: voted final and not voted notar")
		}
	}

	return true, nil
}

func (ss *SlotState) ContainsVote(v *consensus.Vote) bool {
	voter := v.PubKey

	switch v.VoteType {
	case consensus.NOTAR_VOTE:
		if hv, exists := ss.votes.notar[voter]; exists && hv.hash == v.BlockHash {
			return true
		}

	case consensus.NOTAR_FALLBACK_VOTE:
		hvs, exists := ss.votes.notarFallback[voter]
		if !exists {
			return false
		}
		for _, hv := range hvs {
			if hv.hash == v.BlockHash {
				return true
			}
		}

	case consensus.SKIP_VOTE, consensus.SKIP_FALLBACK_VOTE:
		_, skipExists := ss.votes.skip[voter]
		_, skipFallbackExists := ss.votes.skipFallback[voter]
		if skipExists || skipFallbackExists {
			return true
		}

	case consensus.FINAL_VOTE:
		if _, exists := ss.votes.final[voter]; exists {
			return true
		}
	}

	return false
}

func (ss *SlotState) ContainsCert(c *consensus.Cert) bool {
	switch c.CertType {
	case consensus.NOTAR_CERT:
		if ss.certificates.notar != nil {
			return true
		}

	case consensus.NOTAR_FALLBACK_CERT:
		for _, cert := range ss.certificates.notarFallback {
			if cert.BlockHash == c.BlockHash {
				return true
			}
		}

	case consensus.SKIP_CERT:
		if ss.certificates.skip != nil {
			return true
		}

	case consensus.FAST_FINAL_CERT:
		if ss.certificates.fastFinalize != nil {
			return true
		}

	case consensus.FINAL_CERT:
		if ss.certificates.finalize != nil {
			return true
		}

	}

	return false
}

func (ss *SlotState) AddVote(v *consensus.Vote) ([]consensus.Cert, []votor.VotorEvent, []BlockId) {
	voter := v.PubKey

	newVotorEvents := []votor.VotorEvent{}
	newCerts := []consensus.Cert{}
	newRepairBlocks := []BlockId{}

	switch v.VoteType {
	case consensus.NOTAR_VOTE:
		ss.votes.notar[voter] = &HashVotes{hash: v.BlockHash, vote: v}
		newCerts, newVotorEvents, newRepairBlocks = ss.countNotarStake(v.Slot, v.BlockHash)

	case consensus.NOTAR_FALLBACK_VOTE:
		ss.votes.notarFallback[voter] = append(ss.votes.notarFallback[voter], &HashVotes{hash: v.BlockHash, vote: v})
		newCerts = ss.countNotarFallbackStake(v.Slot, v.BlockHash)

	case consensus.SKIP_VOTE:
		ss.votes.skip[voter] = v
		ss.votedStakes.notarOrSkip += 1
		newCerts, newVotorEvents, newRepairBlocks = ss.countSkipStake(v.Slot, false)

	case consensus.SKIP_FALLBACK_VOTE:
		ss.votes.skipFallback[voter] = v
		newCerts, newVotorEvents, newRepairBlocks = ss.countSkipStake(v.Slot, true)

	case consensus.FINAL_VOTE:
		ss.votes.final[voter] = v
		newCerts = ss.countFinalStake(v.Slot)
	}

	if voter == ss.ownPubKey {
		for blockHash := range ss.pendingSafeToNotar {
			if _, exists := ss.sentSafeToNotar[blockHash]; exists {
				continue
			}
			switch ss.checkSafeToNotar(blockHash) {
			case SAFE_TO_NOTAR:
				newVotorEvents = append(newVotorEvents, votor.VotorEvent{
					Type:      votor.SAFE_TO_NOTAR,
					Slot:      v.Slot,
					BlockHash: blockHash,
				})

			case MISSING_BLOCK:
				newRepairBlocks = append(newRepairBlocks, BlockId{Slot: v.Slot, BlockHash: blockHash})

			case AWAITING_VOTES:
				// do nothing, wait for more votes
			}
		}
	}

	return newCerts, newVotorEvents, newRepairBlocks
}

func (ss *SlotState) AddCert(cert *consensus.Cert) {
	switch cert.CertType {
	case consensus.NOTAR_CERT:
		ss.certificates.notar = cert

	case consensus.NOTAR_FALLBACK_CERT:
		if !ss.isNotarFallback(cert.BlockHash) {
			ss.certificates.notarFallback = append(ss.certificates.notarFallback, cert)
		}

	case consensus.SKIP_CERT:
		ss.certificates.skip = cert

	case consensus.FAST_FINAL_CERT:
		ss.certificates.fastFinalize = cert

	case consensus.FINAL_CERT:
		ss.certificates.finalize = cert

	}
}

func (ss *SlotState) NotifyParentKnown(blockHash [32]byte) {
	ss.parents[blockHash] = KNOWN
}

func (ss *SlotState) NotifyParentCertified(blockHash [32]byte) (*votor.VotorEvent, *BlockId) {
	_, ok := ss.parents[blockHash]
	if !ok {
		panic("parent block not known")
	}

	ss.parents[blockHash] = CERTIFIED

	if _, exists := ss.sentSafeToNotar[blockHash]; exists {
		return nil, nil
	}

	switch ss.checkSafeToNotar(blockHash) {
	case SAFE_TO_NOTAR:
		return &votor.VotorEvent{
			Type:      votor.SAFE_TO_NOTAR,
			Slot:      ss.slot,
			BlockHash: blockHash,
		}, nil

	case MISSING_BLOCK:
		return nil, &BlockId{Slot: ss.slot, BlockHash: blockHash}

	case AWAITING_VOTES:
		// do nothing, wait for more votes
	}

	return nil, nil
}

func (ss *SlotState) countNotarStake(slot uint64, blockHash [32]byte) ([]consensus.Cert, []votor.VotorEvent, []BlockId) {
	newVotorEvents := []votor.VotorEvent{}
	newCerts := []consensus.Cert{}
	newRepairBlocks := []BlockId{}

	ss.votedStakes.notar[blockHash] += 1
	ss.votedStakes.notarOrSkip += 1
	ss.votedStakes.topNotar = maxStake(ss.votedStakes.topNotar, ss.votedStakes.notar[blockHash])

	// Check safe to notar
	if _, exists := ss.sentSafeToNotar[blockHash]; !exists {
		switch ss.checkSafeToNotar(blockHash) {
		case SAFE_TO_NOTAR:
			newVotorEvents = append(newVotorEvents, votor.VotorEvent{
				Type:      votor.SAFE_TO_NOTAR,
				Slot:      slot,
				BlockHash: blockHash,
			})

		case MISSING_BLOCK:
			newRepairBlocks = append(newRepairBlocks, BlockId{Slot: slot, BlockHash: blockHash})

		case AWAITING_VOTES:
			// do nothing, wait for more votes
		}
	}

	// Check safe to skip
	if !ss.sentSafeToSkip && ss.isWeakQuorum(ss.votedStakes.notarOrSkip-ss.votedStakes.topNotar) && ss.votes.notar[ss.ownPubKey] != nil {
		ss.sentSafeToSkip = true
		newVotorEvents = append(newVotorEvents, votor.VotorEvent{
			Type: votor.SAFE_TO_SKIP,
			Slot: slot,
		})
	}

	// Check to create certs
	notarStake := ss.votedStakes.notar[blockHash]
	nfStake := ss.votedStakes.notarFallback[blockHash]

	if ss.isQuorum(notarStake+nfStake) && !ss.isNotarFallback(blockHash) {
		nSigns, nPubkeys := ss.getSignsAndPubkeysFromVote(consensus.NOTAR_VOTE, blockHash)
		nfSigns, nfPubkeys := ss.getSignsAndPubkeysFromVote(consensus.NOTAR_FALLBACK_VOTE, blockHash)
		newCert := consensus.Cert{
			Slot:                slot,
			CertType:            consensus.NOTAR_FALLBACK_CERT,
			BlockHash:           blockHash,
			Stake:               notarStake + nfStake,
			ListPubKeys:         nPubkeys,
			ListPubKeysFallback: nfPubkeys,
		}
		newCert.AggregateSignature(nSigns)
		newCert.AggregateSignatureFallback(nfSigns)
		newCerts = append(newCerts, newCert)
	}

	if ss.isQuorum(notarStake) && ss.certificates.notar == nil {
		nSigns, nPubkeys := ss.getSignsAndPubkeysFromVote(consensus.NOTAR_VOTE, blockHash)
		newCert := consensus.Cert{
			Slot:        slot,
			CertType:    consensus.NOTAR_CERT,
			BlockHash:   blockHash,
			Stake:       nfStake,
			ListPubKeys: nPubkeys,
		}
		newCert.AggregateSignature(nSigns)
		newCerts = append(newCerts, newCert)
	}

	if ss.isStrongQuorum(notarStake) && ss.certificates.fastFinalize == nil {
		nSigns, nPubkeys := ss.getSignsAndPubkeysFromVote(consensus.NOTAR_VOTE, blockHash)
		newCert := consensus.Cert{
			Slot:        slot,
			CertType:    consensus.FAST_FINAL_CERT,
			BlockHash:   blockHash,
			Stake:       nfStake,
			ListPubKeys: nPubkeys,
		}
		newCert.AggregateSignature(nSigns)
		newCerts = append(newCerts, newCert)
	}

	return newCerts, newVotorEvents, newRepairBlocks
}

func (ss *SlotState) countNotarFallbackStake(slot uint64, blockHash [32]byte) []consensus.Cert {
	newCerts := []consensus.Cert{}

	ss.votedStakes.notarFallback[blockHash] += 1

	// Check to create cert
	notarStake := ss.votedStakes.notar[blockHash]
	nfStake := ss.votedStakes.notarFallback[blockHash]
	if ss.isQuorum(notarStake+nfStake) && !ss.isNotarFallback(blockHash) {
		nSigns, nPubkeys := ss.getSignsAndPubkeysFromVote(consensus.NOTAR_VOTE, blockHash)
		nfSigns, nfPubkeys := ss.getSignsAndPubkeysFromVote(consensus.NOTAR_FALLBACK_VOTE, blockHash)
		newCert := consensus.Cert{
			Slot:                slot,
			CertType:            consensus.NOTAR_FALLBACK_CERT,
			BlockHash:           blockHash,
			Stake:               notarStake + nfStake,
			ListPubKeys:         nPubkeys,
			ListPubKeysFallback: nfPubkeys,
		}
		newCert.AggregateSignature(nSigns)
		newCert.AggregateSignatureFallback(nfSigns)
		newCerts = append(newCerts, newCert)
	}

	return newCerts
}

func (ss *SlotState) countSkipStake(slot uint64, isFallback bool) ([]consensus.Cert, []votor.VotorEvent, []BlockId) {
	newCerts := []consensus.Cert{}
	newVotorEvents := []votor.VotorEvent{}
	newRepairBlocks := []BlockId{}

	if isFallback {
		ss.votedStakes.skipFallback += 1
	} else {
		ss.votedStakes.skip += 1
	}

	// Check safe to notar
	for blockHash := range ss.pendingSafeToNotar {
		if _, exists := ss.sentSafeToNotar[blockHash]; !exists {
			switch ss.checkSafeToNotar(blockHash) {
			case SAFE_TO_NOTAR:
				newVotorEvents = append(newVotorEvents, votor.VotorEvent{
					Type:      votor.SAFE_TO_NOTAR,
					Slot:      slot,
					BlockHash: blockHash,
				})

			case MISSING_BLOCK:
				newRepairBlocks = append(newRepairBlocks, BlockId{Slot: slot, BlockHash: blockHash})

			case AWAITING_VOTES:
				// do nothing, wait for more votes
			}
		}
	}

	// Check to create skip cert
	totalSkipStake := ss.votedStakes.skip + ss.votedStakes.skipFallback
	if ss.isQuorum(totalSkipStake) && ss.certificates.skip == nil {
		sSigns, sPubkeys := ss.getSignsAndPubkeysFromVote(consensus.SKIP_VOTE, [32]byte{})
		newCert := consensus.Cert{
			Slot:        slot,
			CertType:    consensus.SKIP_CERT,
			Stake:       totalSkipStake,
			ListPubKeys: sPubkeys,
		}
		newCert.AggregateSignature(sSigns)
		newCerts = append(newCerts, newCert)
	}

	// Check safe to skip
	if !ss.sentSafeToSkip && ss.isWeakQuorum(ss.votedStakes.notarOrSkip-ss.votedStakes.topNotar) && ss.votes.notar[ss.ownPubKey] != nil {
		ss.sentSafeToSkip = true
		newVotorEvents = append(newVotorEvents, votor.VotorEvent{
			Type: votor.SAFE_TO_SKIP,
			Slot: slot,
		})
	}

	return newCerts, newVotorEvents, newRepairBlocks
}

func (ss *SlotState) countFinalStake(slot uint64) []consensus.Cert {
	newCerts := []consensus.Cert{}

	ss.votedStakes.final += 1

	// Check to create cert
	if ss.isQuorum(ss.votedStakes.final) && ss.certificates.finalize == nil {
		fSigns, fPubkeys := ss.getSignsAndPubkeysFromVote(consensus.FINAL_VOTE, [32]byte{})
		newCert := consensus.Cert{
			Slot:        slot,
			CertType:    consensus.FINAL_CERT,
			Stake:       ss.votedStakes.final,
			ListPubKeys: fPubkeys,
		}
		newCert.AggregateSignature(fSigns)
		newCerts = append(newCerts, newCert)
	}

	return newCerts
}

func (ss *SlotState) checkSafeToNotar(blockHash [32]byte) SafeToNotarStatus {
	notarStake := ss.votedStakes.notar[blockHash]
	skipStake := ss.votedStakes.skip

	if !ss.isWeakestQuorum(notarStake) {
		return AWAITING_VOTES
	}

	if !ss.isWeakQuorum(notarStake) && !ss.isQuorum(notarStake+skipStake) {
		ss.pendingSafeToNotar[blockHash] = struct{}{}
		return AWAITING_VOTES
	}

	if _, exists := ss.parents[blockHash]; !exists {
		return MISSING_BLOCK
	} else if ss.parents[blockHash] != CERTIFIED {
		return AWAITING_VOTES
	}

	_, isOwnVoteNotar := ss.votes.notar[ss.ownPubKey]
	_, isOwnVoteSkip := ss.votes.skip[ss.ownPubKey]

	if isOwnVoteSkip || (isOwnVoteNotar && ss.votes.notar[ss.ownPubKey].hash != blockHash) {
		ss.sentSafeToNotar[blockHash] = struct{}{}
		delete(ss.pendingSafeToNotar, blockHash)
		return SAFE_TO_NOTAR
	} else {
		if !isOwnVoteNotar && !isOwnVoteSkip {
			ss.pendingSafeToNotar[blockHash] = struct{}{}
		}
		return AWAITING_VOTES
	}
}

func (ss *SlotState) isWeakestQuorum(stake uint64) bool {
	return stake >= TOTAL_NODE/5
}

func (ss *SlotState) isWeakQuorum(stake uint64) bool {
	return stake > (TOTAL_NODE*2)/5
}

func (ss *SlotState) isQuorum(stake uint64) bool {
	return stake > (TOTAL_NODE*3)/5
}

func (ss *SlotState) isStrongQuorum(stake uint64) bool {
	return stake > (TOTAL_NODE*4)/5
}

func (ss *SlotState) isNotarFallback(blockHash [32]byte) bool {
	for _, cert := range ss.certificates.notarFallback {
		if cert.BlockHash == blockHash {
			return true
		}
	}
	return false
}

func (ss *SlotState) getSignsAndPubkeysFromVote(voteType consensus.VoteType, blockHash [32]byte) ([]bls.Sign, []string) {
	var signs []bls.Sign
	var pubkeys []string
	switch voteType {
	case consensus.NOTAR_VOTE:
		for _, voteHash := range ss.votes.notar {
			if voteHash.hash == blockHash {
				if sign, err := utils.BytesToBlsSignature(voteHash.vote.Signature); err == nil {
					signs = append(signs, sign)
				}
				pubkeys = append(pubkeys, voteHash.vote.PubKey)
			}
		}

	case consensus.NOTAR_FALLBACK_VOTE:
		for _, voteHashs := range ss.votes.notarFallback {
			for _, voteHash := range voteHashs {
				if voteHash.hash == blockHash {
					if sign, err := utils.BytesToBlsSignature(voteHash.vote.Signature); err == nil {
						signs = append(signs, sign)
					}
					pubkeys = append(pubkeys, voteHash.vote.PubKey)
				}
			}
		}

	case consensus.SKIP_VOTE:
		for _, vote := range ss.votes.skip {
			if sign, err := utils.BytesToBlsSignature(vote.Signature); err == nil {
				signs = append(signs, sign)
			}
			pubkeys = append(pubkeys, vote.PubKey)
		}

	case consensus.SKIP_FALLBACK_VOTE:
		for _, vote := range ss.votes.skipFallback {
			if sign, err := utils.BytesToBlsSignature(vote.Signature); err == nil {
				signs = append(signs, sign)
			}
			pubkeys = append(pubkeys, vote.PubKey)
		}

	case consensus.FINAL_VOTE:
		for _, vote := range ss.votes.final {
			if sign, err := utils.BytesToBlsSignature(vote.Signature); err == nil {
				signs = append(signs, sign)
			}
			pubkeys = append(pubkeys, vote.PubKey)
		}
	}

	return signs, pubkeys
}

func maxStake(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}
