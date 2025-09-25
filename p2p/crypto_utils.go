package p2p

import (
	"crypto/ed25519"

	"github.com/mezonai/mmn/common"
	"github.com/pkg/errors"
)

func GetPublicKeyFromBase58(id string, idType string) (ed25519.PublicKey, error) {
	if id == "" {
		return nil, errors.Errorf("%s cannot be empty", idType)
	}

	pubKeyBytes, err := common.DecodeBase58ToBytes(id)
	if err != nil {
		return nil, errors.Errorf("failed to decode %s: %w", idType, err)
	}

	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return nil, errors.Errorf("invalid %s length: expected %d, got %d", idType, ed25519.PublicKeySize, len(pubKeyBytes))
	}

	return ed25519.PublicKey(pubKeyBytes), nil
}

func GetLeaderPublicKey(leaderID string) (ed25519.PublicKey, error) {
	return GetPublicKeyFromBase58(leaderID, "leader ID")
}

func GetVoterPublicKey(voterID string) (ed25519.PublicKey, error) {
	return GetPublicKeyFromBase58(voterID, "voter ID")
}
