package consensus

import (
	"crypto/ed25519"
	"fmt"

	"github.com/mezonai/mmn/jsonx"
	"github.com/mr-tron/base58"
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

// VerifySignature check vote signature with public key
func (v *Vote) verifySignature(pub ed25519.PublicKey) bool {
	return ed25519.Verify(pub, v.serializeVote(), v.Signature)
}

// Validate basic checks (nonce, slot >= 0, etc)
func (v *Vote) Validate() error {
	if len(v.Signature) == 0 {
		return fmt.Errorf("missing signature")
	}
	pub, err := base58.Decode(v.VoterID)
	if err != nil {
		return fmt.Errorf("invalid voterID")
	}
	if !v.verifySignature(pub) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}
