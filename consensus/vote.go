package consensus

import (
	"crypto/ed25519"
	"fmt"

	"github.com/mezonai/mmn/common"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
	"github.com/pkg/errors"
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

// getPublicKeyFromVoterID extracts public key from VoterID (Base58 encoded)
func getPublicKeyFromVoterID(voterID string) (ed25519.PublicKey, error) {
	if voterID == "" {
		return nil, errors.New("voter ID cannot be empty")
	}

	pubKeyBytes, err := common.DecodeBase58ToBytes(voterID)
	if err != nil {
		return nil, errors.Errorf("failed to decode voter ID: %w", err)
	}

	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return nil, errors.Errorf("invalid voter ID length: expected %d, got %d", ed25519.PublicKeySize, len(pubKeyBytes))
	}

	return ed25519.PublicKey(pubKeyBytes), nil
}

func (v *Vote) VerifySignature() bool {
	pubKey, err := getPublicKeyFromVoterID(v.VoterID)
	if err != nil {
		logx.Error("Vote", fmt.Sprintf("Failed to get public key from VoterID: %v", err))
		return false
	}

	if len(pubKey) != ed25519.PublicKeySize {
		return false
	}
	if len(v.Signature) != ed25519.SignatureSize {
		return false
	}

	return ed25519.Verify(pubKey, v.serializeVote(), v.Signature)
}

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
