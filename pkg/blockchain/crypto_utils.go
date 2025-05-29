// File: pkg/blockchain/crypto_utils.go
package blockchain

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
)

// GenerateKeyPair creates a new ECDSA key pair.
func GenerateKeyPair() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// SignTransaction signs a transaction using the provided private key.
func SignTransaction(tx *Transaction, privKey *ecdsa.PrivateKey) (string, error) {
	txHash := sha256.Sum256([]byte(tx.String()))
	r, s, err := ecdsa.Sign(rand.Reader, privKey, txHash[:])
	if err != nil {
		return "", err
	}
	// Serialize signature (concatenate r and s)
	signature := append(r.Bytes(), s.Bytes()...)
	return hex.EncodeToString(signature), nil
}

// VerifyTransactionSignature verifies that the transaction signature is valid.
func VerifyTransactionSignature(tx *Transaction, pubKey *ecdsa.PublicKey) bool {
	if tx.Signature == "" {
		return false
	}
	sigBytes, err := hex.DecodeString(tx.Signature)
	if err != nil {
		return false
	}
	txHash := sha256.Sum256([]byte(tx.String()))
	// Assuming r and s are of equal length.
	sigLen := len(sigBytes)
	if sigLen%2 != 0 {
		return false
	}
	r := new(big.Int).SetBytes(sigBytes[:sigLen/2])
	s := new(big.Int).SetBytes(sigBytes[sigLen/2:])
	return ecdsa.Verify(pubKey, txHash[:], r, s)
}
