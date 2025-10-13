package consensus

import (
	"crypto/ed25519"
	"fmt"

	"github.com/mezonai/mmn/jsonx"
)

// Vote is a vote for a block of a slot
type Vote struct {
	Slot      uint64 // slot number
	BlockHash [32]byte
	VoterID   string // leaderID or validatorID
	Signature []byte
}

// serializeVote to sign and verify (without Signature)
func (v *Vote) serializeVote() []byte {
	data, _ := jsonx.Marshal(struct {
		Slot      uint64
		BlockHash [32]byte
		VoterID   string
	}{
		Slot:      v.Slot,
		BlockHash: v.BlockHash,
		VoterID:   v.VoterID,
	})
	return data
}

// Sign vote with private key of voter
func (v *Vote) Sign(priv ed25519.PrivateKey) {
	v.Signature = ed25519.Sign(priv, v.serializeVote())
}

// Validate basic checks (nonce, slot >= 0, etc)
func (v *Vote) Validate() error {
	if len(v.Signature) == 0 {
		return fmt.Errorf("missing signature")
	}
	if len(v.Signature) != ed25519.SignatureSize {
		return fmt.Errorf("invalid signature size: expected %d, got %d", ed25519.SignatureSize, len(v.Signature))
	}
	if v.VoterID == "" {
		return fmt.Errorf("voter ID cannot be empty")
	}
	return nil
}
