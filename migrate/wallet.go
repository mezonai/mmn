package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
)

// CreateWallet generates a new Ed25519 wallet
func CreateWallet() (*Wallet, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %v", err)
	}

	// Create address from public key hash
	hash := sha256.Sum256(publicKey)
	address := hex.EncodeToString(hash[:])

	return &Wallet{
		PublicKey:  publicKey,
		PrivateKey: privateKey,
		Address:    address,
	}, nil
}

// getFaucetAccount returns the hardcoded faucet account for testing
func GetFaucetAccount() (string, ed25519.PrivateKey) {
	faucetPrivateKeyHex := "302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee"
	faucetPrivateKeyDer, err := hex.DecodeString(faucetPrivateKeyHex)
	if err != nil {
		log.Fatalf("Failed to decode faucet private key: %v", err)
	}

	// Extract the last 32 bytes as the Ed25519 seed
	faucetSeed := faucetPrivateKeyDer[len(faucetPrivateKeyDer)-32:]
	faucetPrivateKey := ed25519.NewKeyFromSeed(faucetSeed)
	faucetPublicKey := faucetPrivateKey.Public().(ed25519.PublicKey)
	faucetPublicKeyHex := hex.EncodeToString(faucetPublicKey[:])
	return faucetPublicKeyHex, faucetPrivateKey
}

// EncryptPrivateKey encrypts a private key using AES-GCM with the master key
func EncryptPrivateKey(privateKey ed25519.PrivateKey, masterKey string) (string, error) {
	// Decode master key from base64
	keyBytes, err := base64.StdEncoding.DecodeString(masterKey)
	if err != nil {
		return "", fmt.Errorf("failed to decode master key: %v", err)
	}

	// Create AES cipher
	block, err := aes.NewCipher(keyBytes[:32]) // Use first 32 bytes for AES-256
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %v", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %v", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %v", err)
	}

	// Encrypt private key
	ciphertext := gcm.Seal(nonce, nonce, privateKey, nil)

	// Return base64 encoded result
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptPrivateKey decrypts a private key using AES-GCM with the master key
func DecryptPrivateKey(encryptedKey, masterKey string) (ed25519.PrivateKey, error) {
	// Decode encrypted key from base64
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode encrypted key: %v", err)
	}

	// Decode master key from base64
	keyBytes, err := base64.StdEncoding.DecodeString(masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode master key: %v", err)
	}

	// Create AES cipher
	block, err := aes.NewCipher(keyBytes[:32]) // Use first 32 bytes for AES-256
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %v", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %v", err)
	}

	// Extract nonce and encrypted data
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	// Decrypt private key
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %v", err)
	}

	return ed25519.PrivateKey(plaintext), nil
}