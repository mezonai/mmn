package faucet

import (
	"crypto/sha256"
	"fmt"

	"github.com/mezonai/mmn/types"
	"github.com/mr-tron/base58"
)

func CreateMultisigConfig(threshold int, signers []string) (*types.MultisigConfig, error) {
	if len(signers) < 2 {
		return nil, fmt.Errorf("invalid signers: %d", len(signers))
	}

	if threshold <= 0 || threshold > len(signers) {
		return nil, fmt.Errorf("invalid threshold: %d", threshold)
	}

	address, err := generateMultisigAddress(threshold, signers)
	if err != nil {
		return nil, fmt.Errorf("failed to generate multisig address: %w", err)
	}

	return &types.MultisigConfig{
		Signers: signers,
		Address: address,
	}, nil
}

func generateMultisigAddress(threshold int, signers []string) (string, error) {
	configData := fmt.Sprintf("multisig:%d:%s", threshold, signers[0])
	for i := 1; i < len(signers); i++ {
		configData += ":" + signers[i]
	}

	hash := sha256.Sum256([]byte(configData))
	return base58.Encode(hash[:]), nil
}
