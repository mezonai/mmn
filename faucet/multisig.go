package faucet

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/mezonai/mmn/types"
	"github.com/mr-tron/base58"
)

func CreateMultisigConfig(threshold int, signers []string) (*types.MultisigConfig, error) {

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
	var configData string
	if len(signers) == 0 {
		configData = fmt.Sprintf("multisig:%d", threshold)
	} else {
		configData = fmt.Sprintf("multisig:%d:%s", threshold, strings.Join(signers, ":"))
	}

	hash := sha256.Sum256([]byte(configData))
	return base58.Encode(hash[:]), nil
}
