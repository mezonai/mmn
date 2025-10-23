package faucet

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/types"
	"github.com/mr-tron/base58"
)

var (
	ErrInvalidThreshold        = errors.New("multisig: threshold must be > 0 and <= number of signers")
	ErrInvalidSigners          = errors.New("multisig: must have at least 2 signers")
	ErrDuplicateSigner         = errors.New("multisig: duplicate signer public key")
	ErrInvalidSignature        = errors.New("multisig: invalid signature")
	ErrInsufficientSignatures  = errors.New("multisig: insufficient signatures")
	ErrInvalidSigner           = errors.New("multisig: signer not authorized")
	ErrInvalidMultisigTx       = errors.New("multisig: invalid multisig transaction")
	ErrInvalidAddress          = errors.New("whitelist: invalid address")
	ErrRecipientNotInWhitelist = errors.New("whitelist: recipient not in whitelist")
	ErrRecipientAlreadyExists  = errors.New("whitelist: recipient already exists")
)

func CreateMultisigConfig(threshold int, signers []string) (*types.MultisigConfig, error) {
	if len(signers) < 2 {
		return nil, ErrInvalidSigners
	}

	if threshold <= 0 || threshold > len(signers) {
		return nil, ErrInvalidThreshold
	}

	uniqueSigners := make([]string, 0, len(signers))
	seen := make(map[string]bool)

	for _, signer := range signers {
		if err := validatePublicKey(signer); err != nil {
			return nil, fmt.Errorf("invalid signer public key %s: %w", signer, err)
		}

		if seen[signer] {
			return nil, ErrDuplicateSigner
		}
		seen[signer] = true
		uniqueSigners = append(uniqueSigners, signer)
	}

	sort.Strings(uniqueSigners)

	address, err := generateMultisigAddress(threshold, uniqueSigners)
	if err != nil {
		return nil, fmt.Errorf("failed to generate multisig address: %w", err)
	}

	return &types.MultisigConfig{
		Threshold: threshold,
		Signers:   uniqueSigners,
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

func validatePublicKey(pubKeyStr string) error {
	decoded, err := base58.Decode(pubKeyStr)
	if err != nil {
		return fmt.Errorf("invalid base58: %w", err)
	}

	if len(decoded) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key length: expected %d, got %d", ed25519.PublicKeySize, len(decoded))
	}

	return nil
}

func SignMultisigTx(tx *types.MultisigTx, signerPubKey string, privKey []byte) (*types.MultisigSignature, error) {
	if !isAuthorizedSigner(&tx.Config, signerPubKey) {
		return nil, ErrInvalidSigner
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
		return ErrInvalidSigner
	}

	if !verifyMultisigSignature(tx, sig) {
		return ErrInvalidSignature
	}

	tx.Signatures = append(tx.Signatures, *sig)
	return nil
}

func VerifyMultisigTx(tx *types.MultisigTx) error {
	if len(tx.Signatures) < tx.Config.Threshold {
		return ErrInsufficientSignatures
	}

	validSignatures := 0
	for _, sig := range tx.Signatures {
		if verifyMultisigSignature(tx, &sig) {
			validSignatures++
		}
	}

	if validSignatures < tx.Config.Threshold {
		return ErrInsufficientSignatures
	}

	return nil
}

func verifyMultisigSignature(tx *types.MultisigTx, sig *types.MultisigSignature) bool {
	pubKeyBytes, err := base58.Decode(sig.Signer)
	if err != nil {
		return false
	}

	sigBytes, err := hex.DecodeString(sig.Signature)
	if err != nil {
		return false
	}

	// Use the same message format as client signing
	message := "faucet_action:add_signature"

	return ed25519.Verify(ed25519.PublicKey(pubKeyBytes), []byte(message), sigBytes)
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
