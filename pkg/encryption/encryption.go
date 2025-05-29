// File: pkg/encryption/encryption.go
package encryption

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// --- Key Derivation ---

// deriveKey derives a 32-byte key from the provided secret using SHA‑256.
func deriveKey(secret string) []byte {
	hash := sha256.Sum256([]byte(secret))
	return hash[:]
}

// --- Custom Layer (Simplified as Identity) ---

// customLayerOutput holds the result of the custom layer along with the original length.
type customLayerOutput struct {
	Ciphertext     string
	OriginalLength int
}

// applyCustomLayer is our identity function for the inner layer.
// It simply returns the plaintext as the "custom ciphertext".
func applyCustomLayer(plaintext, matrixSecret, dictSecret, transformSecret string, chunkSize int) customLayerOutput {
	return customLayerOutput{
		Ciphertext:     plaintext, // No modification.
		OriginalLength: len(plaintext),
	}
}

// reverseCustomLayer simply trims the input to the original length (identity function).
func reverseCustomLayer(ciphertext, matrixSecret, dictSecret, transformSecret string, chunkSize int, origLen int) string {
	if len(ciphertext) < origLen {
		return ciphertext
	}
	return ciphertext[:origLen]
}

// --- Outer ChaCha20-Poly1305 Encryption Layer ---

// outerEncrypt encrypts the provided customLayerOutput.Ciphertext using ChaCha20-Poly1305.
func outerEncrypt(customOut customLayerOutput, outerKey []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(outerKey)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, chacha20poly1305.NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	plainBlob := []byte(customOut.Ciphertext)
	ciphertext := aead.Seal(nonce, nonce, plainBlob, nil)
	return ciphertext, nil
}

// outerDecrypt decrypts the provided ciphertext using ChaCha20-Poly1305.
// It returns the decrypted (custom) ciphertext along with its length.
func outerDecrypt(outerCiphertext, outerKey []byte) (string, int, error) {
	aead, err := chacha20poly1305.New(outerKey)
	if err != nil {
		return "", 0, err
	}
	if len(outerCiphertext) < chacha20poly1305.NonceSize {
		return "", 0, fmt.Errorf("ciphertext too short")
	}
	nonce := outerCiphertext[:chacha20poly1305.NonceSize]
	encrypted := outerCiphertext[chacha20poly1305.NonceSize:]
	plainBlob, err := aead.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", 0, err
	}
	return string(plainBlob), len(plainBlob), nil
}

// --- Exported Functions ---

// Encrypt encrypts the given plaintext using the simplified two-layer scheme.
// It preserves the function signature so that existing code remains unchanged.
func Encrypt(plainText []byte) (string, error) {
	// Convert plaintext to string.
	plaintextStr := string(plainText)

	// Parameters (you can later make these configurable).
	matrixSecret := "MatrixSecretForSubstitution"
	dictSecret := "DictionarySecretForUnknownSymbols"
	transformSecret := "TransformSecretForChunks"
	outerSecret := "OuterLayerSecretForChaCha20Poly1305"
	chunkSize := 4

	// Custom inner layer: simplified as identity.
	customOut := applyCustomLayer(plaintextStr, matrixSecret, dictSecret, transformSecret, chunkSize)

	// Derive the outer key.
	outerKey := deriveKey(outerSecret)

	// Apply outer encryption.
	cipherBytes, err := outerEncrypt(customOut, outerKey)
	if err != nil {
		return "", err
	}
	// Return the ciphertext as a hex-encoded string.
	return fmt.Sprintf("%x", cipherBytes), nil
}

// Decrypt decrypts the given hex-encoded ciphertext using the simplified scheme.
func Decrypt(cipherHex string) ([]byte, error) {
	outerSecret := "OuterLayerSecretForChaCha20Poly1305"
	matrixSecret := "MatrixSecretForSubstitution"
	dictSecret := "DictionarySecretForUnknownSymbols"
	transformSecret := "TransformSecretForChunks"
	chunkSize := 4

	outerKey := deriveKey(outerSecret)

	// Decode the hex-encoded ciphertext.
	cipherBytes, err := hex.DecodeString(cipherHex)
	if err != nil {
		return nil, err
	}

	// Outer decryption.
	customText, origLen, err := outerDecrypt(cipherBytes, outerKey)
	if err != nil {
		return nil, err
	}

	// Reverse the custom layer (identity function).
	plainTextStr := reverseCustomLayer(customText, matrixSecret, dictSecret, transformSecret, chunkSize, origLen)
	return []byte(plainTextStr), nil
}
