package consensus

import (
	"crypto/ed25519"
	"fmt"

	"github.com/mezonai/mmn/jsonx"
)

type VoteType int

const (
	NOTAR VoteType = iota
	NOTAR_FALLBACK
	SKIP
	SKIP_FALLBACK
	FINAL
)

// Vote is a vote for a block of a slot
type Vote struct {
	Slot      uint64 // slot number
	VoteType  VoteType
	BlockHash [32]byte
	VoterID   string // leaderID or validatorID
	Signature []byte
}

// serializeVote to sign and verify (without Signature)
func (v *Vote) serializeVote() []byte {
	data, _ := jsonx.Marshal(struct {
		Slot      uint64
		VoteType  VoteType
		BlockHash [32]byte
		VoterID   string
	}{
		Slot:      v.Slot,
		VoteType:  v.VoteType,
		BlockHash: v.BlockHash,
		VoterID:   v.VoterID,
	})
	return data
}

// Sign vote with private key of voter
// TODO: sign with BLS in the same message to support aggregation
func (v *Vote) Sign(priv ed25519.PrivateKey) {
	v.Signature = ed25519.Sign(priv, v.serializeVote())
}

// VerifySignature check vote signature with public key
// TODO: update
func (v *Vote) VerifySignature(pub ed25519.PublicKey) bool {
	return ed25519.Verify(pub, v.serializeVote(), v.Signature)
}

// Validate basic checks (nonce, slot >= 0, etc)
func (v *Vote) Validate() error {
	if len(v.Signature) == 0 {
		return fmt.Errorf("missing signature")
	}
	return nil
}
