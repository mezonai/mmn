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
	ErrInvalidThreshold     = errors.New("multisig: threshold must be > 0 and <= number of signers")
	ErrInvalidSigners       = errors.New("multisig: must have at least 2 signers")
	ErrDuplicateSigner      = errors.New("multisig: duplicate signer public key")
	ErrInvalidSignature     = errors.New("multisig: invalid signature")
	ErrInsufficientSignatures = errors.New("multisig: insufficient signatures")
	ErrInvalidSigner        = errors.New("multisig: signer not authorized")
	ErrInvalidMultisigTx    = errors.New("multisig: invalid multisig transaction")
)

// MultisigConfig represents the configuration for a multisig wallet
type MultisigConfig struct {
	Threshold int      `json:"threshold"` // m in m-of-n
	Signers   []string `json:"signers"`   // n public keys in base58
	Address   string   `json:"address"`   // derived multisig address
}

// MultisigSignature represents a single signature in a multisig transaction
type MultisigSignature struct {
	Signer    string `json:"signer"`    // public key of the signer
	Signature string `json:"signature"` // signature in base58
}

// MultisigTx represents a multisig transaction
type MultisigTx struct {
	Type        int                 `json:"type"`
	Sender      string              `json:"sender"`      // multisig address
	Recipient   string              `json:"recipient"`
	Amount      *uint256.Int        `json:"amount"`
	Timestamp   uint64              `json:"timestamp"`
	TextData    string              `json:"text_data"`
	Nonce       uint64              `json:"nonce"`
	ExtraInfo   string              `json:"extra_info"`
	Signatures  []MultisigSignature `json:"signatures"`
	Config      MultisigConfig      `json:"config"`
}

// CreateMultisigConfig creates a new multisig configuration
func CreateMultisigConfig(threshold int, signers []string) (*MultisigConfig, error) {
	if len(signers) < 2 {
		return nil, ErrInvalidSigners
	}
	
	if threshold <= 0 || threshold > len(signers) {
		return nil, ErrInvalidThreshold
	}

	// Validate and deduplicate signers
	uniqueSigners := make([]string, 0, len(signers))
	seen := make(map[string]bool)
	
	for _, signer := range signers {
		// Validate public key format
		if err := validatePublicKey(signer); err != nil {
			return nil, fmt.Errorf("invalid signer public key %s: %w", signer, err)
		}
		
		if seen[signer] {
			return nil, ErrDuplicateSigner
		}
		seen[signer] = true
		uniqueSigners = append(uniqueSigners, signer)
	}

	// Sort signers for deterministic address generation
	sort.Strings(uniqueSigners)

	// Generate multisig address
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

// generateMultisigAddress creates a deterministic address from multisig configuration
func generateMultisigAddress(threshold int, signers []string) (string, error) {
	// Create a deterministic string representation
	configData := fmt.Sprintf("multisig:%d:%s", threshold, signers[0])
	for i := 1; i < len(signers); i++ {
		configData += ":" + signers[i]
	}
	
	// Hash the configuration to create address
	hash := sha256.Sum256([]byte(configData))
	return base58.Encode(hash[:]), nil
}

// validatePublicKey validates if a string is a valid Ed25519 public key
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

// SignMultisigTx signs a multisig transaction with a private key
func SignMultisigTx(tx *MultisigTx, signerPubKey string, privKey []byte) (*MultisigSignature, error) {
	// Validate the signer is authorized
	if !isAuthorizedSigner(&tx.Config, signerPubKey) {
		return nil, ErrInvalidSigner
	}

	// Convert private key to Ed25519 format
	var ed25519PrivKey ed25519.PrivateKey
	switch l := len(privKey); l {
	case ed25519.SeedSize:
		ed25519PrivKey = ed25519.NewKeyFromSeed(privKey)
	case ed25519.PrivateKeySize:
		ed25519PrivKey = ed25519.PrivateKey(privKey)
	default:
		return nil, fmt.Errorf("unsupported private key length: %d", l)
	}

	// Serialize transaction for signing
	txData := serializeMultisigTx(tx)
	
	// Sign the transaction
	signature := ed25519.Sign(ed25519PrivKey, txData)
	
	return &MultisigSignature{
		Signer:    signerPubKey,
		Signature: base58.Encode(signature),
	}, nil
}

// AddSignature adds a signature to a multisig transaction
func AddSignature(tx *MultisigTx, sig *MultisigSignature) error {
	// Check if signer is already present
	for _, existingSig := range tx.Signatures {
		if existingSig.Signer == sig.Signer {
			return fmt.Errorf("signature from %s already exists", sig.Signer)
		}
	}

	// Validate the signer is authorized
	if !isAuthorizedSigner(&tx.Config, sig.Signer) {
		return ErrInvalidSigner
	}

	// Verify the signature
	if !verifyMultisigSignature(tx, sig) {
		return ErrInvalidSignature
	}

	// Add the signature
	tx.Signatures = append(tx.Signatures, *sig)
	return nil
}

// VerifyMultisigTx verifies if a multisig transaction has enough valid signatures
func VerifyMultisigTx(tx *MultisigTx) error {
	// Check if we have enough signatures
	if len(tx.Signatures) < tx.Config.Threshold {
		return ErrInsufficientSignatures
	}

	// Verify each signature
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

// verifyMultisigSignature verifies a single signature
func verifyMultisigSignature(tx *MultisigTx, sig *MultisigSignature) bool {
	// Decode public key
	pubKeyBytes, err := base58.Decode(sig.Signer)
	if err != nil {
		return false
	}

	// Decode signature
	sigBytes, err := base58.Decode(sig.Signature)
	if err != nil {
		return false
	}

	// Serialize transaction for verification
	txData := serializeMultisigTx(tx)

	// Verify signature
	return ed25519.Verify(ed25519.PublicKey(pubKeyBytes), txData, sigBytes)
}

// isAuthorizedSigner checks if a public key is authorized to sign
func isAuthorizedSigner(config *MultisigConfig, pubKey string) bool {
	for _, signer := range config.Signers {
		if signer == pubKey {
			return true
		}
	}
	return false
}

// serializeMultisigTx serializes a multisig transaction for signing/verification
func serializeMultisigTx(tx *MultisigTx) []byte {
	amountStr := "0"
	if tx.Amount != nil {
		amountStr = tx.Amount.String()
	}
	
	// Create deterministic serialization (exclude signatures to avoid circular dependency)
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

// CreateMultisigFaucetTx creates a new multisig faucet transaction
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

// GetMultisigAddress returns the multisig address from configuration
func (config *MultisigConfig) GetMultisigAddress() string {
	return config.Address
}

// GetSigners returns the list of authorized signers
func (config *MultisigConfig) GetSigners() []string {
	return config.Signers
}

// GetThreshold returns the required number of signatures
func (config *MultisigConfig) GetThreshold() int {
	return config.Threshold
}

// IsComplete checks if the transaction has enough signatures
func (tx *MultisigTx) IsComplete() bool {
	return len(tx.Signatures) >= tx.Config.Threshold
}

// GetSignatureCount returns the number of signatures
func (tx *MultisigTx) GetSignatureCount() int {
	return len(tx.Signatures)
}

// GetRequiredSignatureCount returns the required number of signatures
func (tx *MultisigTx) GetRequiredSignatureCount() int {
	return tx.Config.Threshold
}
