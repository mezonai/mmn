package consensus

import (
	"fmt"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/mezonai/mmn/jsonx"
)

type VoteType int

const (
	NOTAR_VOTE VoteType = iota
	NOTAR_FALLBACK_VOTE
	SKIP_VOTE
	SKIP_FALLBACK_VOTE
	FINAL_VOTE
)

// Vote is a vote for a block of a slot
type Vote struct {
	Slot      uint64 // slot number
	VoteType  VoteType
	BlockHash [32]byte
	PubKey    string // BLS public key of voter
	Signature []byte
}

// serializeVote to sign and verify (without Signature + PubKey to keep the same message)
func (v *Vote) serializeVote() []byte {
	data, _ := jsonx.Marshal(struct {
		Slot      uint64
		VoteType  VoteType
		BlockHash [32]byte
	}{
		Slot:      v.Slot,
		VoteType:  v.VoteType,
		BlockHash: v.BlockHash,
	})
	return data
}

// Sign vote with private key of voter
func (v *Vote) Sign(priv bls.SecretKey) {
	v.Signature = priv.SignByte(v.serializeVote()).Serialize()
}

// VerifySignature check vote signature with public key
func (v *Vote) VerifySignature(pub bls.PublicKey) bool {
	var sign bls.Sign
	if err := sign.Deserialize(v.Signature); err != nil {
		return false
	}
	return sign.VerifyByte(&pub, v.serializeVote())
}

// Validate basic checks (nonce, slot >= 0, etc)
func (v *Vote) Validate() error {
	if len(v.Signature) == 0 {
		return fmt.Errorf("missing signature")
	}
	return nil
}
