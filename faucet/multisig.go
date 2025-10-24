package faucet

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/holiman/uint256"
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
		Threshold: threshold,
		Signers:   signers,
		Address:   address,
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

func SignMultisigTx(tx *types.MultisigTx, signerPubKey string, privKey []byte) (*types.MultisigSignature, error) {
	if !isAuthorizedSigner(&tx.Config, signerPubKey) {
		return nil, fmt.Errorf("signer %s is not authorized", signerPubKey)
	}

	var ed25519PrivKey ed25519.PrivateKey
	switch l := len(privKey); l {
	case ed25519.SeedSize:
		ed25519PrivKey = ed25519.NewKeyFromSeed(privKey)
	case ed25519.PrivateKeySize:
		ed25519PrivKey = ed25519.PrivateKey(privKey)
	default:
		return nil, fmt.Errorf("unsupported private key length: %d", l)
	}

	txData := serializeMultisigTx(tx)

	signature := ed25519.Sign(ed25519PrivKey, txData)

	return &types.MultisigSignature{
		Signer:    signerPubKey,
		Signature: base58.Encode(signature),
	}, nil
}

func AddSignature(tx *types.MultisigTx, sig *types.MultisigSignature) error {
	for _, existingSig := range tx.Signatures {
		if existingSig.Signer == sig.Signer {
			return fmt.Errorf("signature from %s already exists", sig.Signer)
		}
	}

	if !isAuthorizedSigner(&tx.Config, sig.Signer) {
		return fmt.Errorf("signer %s is not authorized", sig.Signer)
	}

	if !verifyMultisigSignature(sig) {
		return fmt.Errorf("invalid signature from %s", sig.Signer)
	}

	tx.Signatures = append(tx.Signatures, *sig)
	return nil
}

func VerifyMultisigTx(tx *types.MultisigTx) error {
	if len(tx.Signatures) < tx.Config.Threshold {
		return fmt.Errorf("insufficient signatures: %d < %d", len(tx.Signatures), tx.Config.Threshold)
	}

	validSignatures := 0
	for _, sig := range tx.Signatures {
		if verifyMultisigSignature(&sig) {
			validSignatures++
		}
	}

	if validSignatures < tx.Config.Threshold {
		return fmt.Errorf("insufficient signatures: %d < %d", validSignatures, tx.Config.Threshold)
	}

	return nil
}

func verifyMultisigSignature(sig *types.MultisigSignature) bool {
	pubKeyBytes, err := base58.Decode(sig.Signer)
	if err != nil {
		return false
	}

	sigBytes, err := hex.DecodeString(sig.Signature)
	if err != nil {
		return false
	}

	message := fmt.Sprintf("%s:%s", FAUCET_ACTION, ADD_SIGNATURE)
	result := ed25519.Verify(ed25519.PublicKey(pubKeyBytes), []byte(message), sigBytes)

	return result
}

func isAuthorizedSigner(config *types.MultisigConfig, pubKey string) bool {
	for _, signer := range config.Signers {
		if signer == pubKey {
			return true
		}
	}
	return false
}

func serializeMultisigTx(tx *types.MultisigTx) []byte {
	amountStr := "0"
	if tx.Amount != nil {
		amountStr = tx.Amount.String()
	}

	metadata := fmt.Sprintf(
		"%d|%s|%s|%s|%s|%d|%s|%d|%d",
		tx.Type,
		tx.Sender,
		tx.Recipient,
		amountStr,
		tx.TextData,
		tx.Nonce,
		tx.ExtraInfo,
		tx.Config.Threshold,
		len(tx.Config.Signers),
	)

	return []byte(metadata)
}

func CreateMultisigFaucetTx(config *types.MultisigConfig, recipient string, amount *uint256.Int, nonce uint64, timestamp uint64, textData string) *types.MultisigTx {
	return &types.MultisigTx{
		Type:       1, // TxTypeFaucet
		Sender:     config.Address,
		Recipient:  recipient,
		Amount:     amount,
		Timestamp:  timestamp,
		TextData:   textData,
		Nonce:      nonce,
		ExtraInfo:  "",
		Signatures: make([]types.MultisigSignature, 0),
		Config:     *config,
	}
}
