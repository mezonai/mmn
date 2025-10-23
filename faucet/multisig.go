package faucet

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"

	"github.com/holiman/uint256"
	"github.com/mr-tron/base58"
)

var (
	ErrInvalidThreshold       = errors.New("multisig: threshold must be > 0 and <= number of signers")
	ErrInvalidSigners         = errors.New("multisig: must have at least 2 signers")
	ErrDuplicateSigner        = errors.New("multisig: duplicate signer public key")
	ErrInvalidSignature       = errors.New("multisig: invalid signature")
	ErrInsufficientSignatures = errors.New("multisig: insufficient signatures")
	ErrInvalidSigner          = errors.New("multisig: signer not authorized")
	ErrInvalidMultisigTx      = errors.New("multisig: invalid multisig transaction")
)

type MultisigConfig struct {
	Threshold int      `json:"threshold"`
	Signers   []string `json:"signers"`
	Address   string   `json:"address"`
}

type MultisigSignature struct {
	Signer    string `json:"signer"`
	Signature string `json:"signature"`
}

type MultisigTx struct {
	Type       int                 `json:"type"`
	Sender     string              `json:"sender"`
	Recipient  string              `json:"recipient"`
	Amount     *uint256.Int        `json:"amount"`
	Timestamp  uint64              `json:"timestamp"`
	TextData   string              `json:"text_data"`
	Nonce      uint64              `json:"nonce"`
	ExtraInfo  string              `json:"extra_info"`
	Signatures []MultisigSignature `json:"signatures"`
	Config     MultisigConfig      `json:"config"`
}

func CreateMultisigConfig(threshold int, signers []string) (*MultisigConfig, error) {
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

	return &MultisigConfig{
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

func SignMultisigTx(tx *MultisigTx, signerPubKey string, privKey []byte) (*MultisigSignature, error) {
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

	return &MultisigSignature{
		Signer:    signerPubKey,
		Signature: base58.Encode(signature),
	}, nil
}

func AddSignature(tx *MultisigTx, sig *MultisigSignature) error {
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

func VerifyMultisigTx(tx *MultisigTx) error {
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

func verifyMultisigSignature(tx *MultisigTx, sig *MultisigSignature) bool {
	pubKeyBytes, err := base58.Decode(sig.Signer)
	if err != nil {
		return false
	}

	sigBytes, err := base58.Decode(sig.Signature)
	if err != nil {
		return false
	}

	txData := serializeMultisigTx(tx)

	return ed25519.Verify(ed25519.PublicKey(pubKeyBytes), txData, sigBytes)
}

func isAuthorizedSigner(config *MultisigConfig, pubKey string) bool {
	for _, signer := range config.Signers {
		if signer == pubKey {
			return true
		}
	}
	return false
}

func serializeMultisigTx(tx *MultisigTx) []byte {
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

func CreateMultisigFaucetTx(config *MultisigConfig, recipient string, amount *uint256.Int, nonce uint64, timestamp uint64, textData string) *MultisigTx {
	return &MultisigTx{
		Type:       1, // TxTypeFaucet
		Sender:     config.Address,
		Recipient:  recipient,
		Amount:     amount,
		Timestamp:  timestamp,
		TextData:   textData,
		Nonce:      nonce,
		ExtraInfo:  "",
		Signatures: make([]MultisigSignature, 0),
		Config:     *config,
	}
}

func (config *MultisigConfig) GetMultisigAddress() string {
	return config.Address
}

func (config *MultisigConfig) GetSigners() []string {
	return config.Signers
}

func (config *MultisigConfig) GetThreshold() int {
	return config.Threshold
}

func (tx *MultisigTx) IsComplete() bool {
	return len(tx.Signatures) >= tx.Config.Threshold
}

func (tx *MultisigTx) GetSignatureCount() int {
	return len(tx.Signatures)
}

func (tx *MultisigTx) GetRequiredSignatureCount() int {
	return tx.Config.Threshold
}
