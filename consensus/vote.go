package consensus

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
)

// Vote là phiếu bầu cho block của một slot
type Vote struct {
	Slot      uint64 // slot số bao nhiêu
	BlockHash [32]byte
	VoterID   string // leaderID hoặc validatorID
	Signature []byte
}

// serializeVote để ký và verify (bỏ Signature)
func (v *Vote) serializeVote() []byte {
	data, _ := json.Marshal(struct {
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

// Sign ký vote bằng private key của voter
func (v *Vote) Sign(priv ed25519.PrivateKey) {
	v.Signature = ed25519.Sign(priv, v.serializeVote())
}

// VerifySignature kiểm tra chữ ký vote với public key
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
