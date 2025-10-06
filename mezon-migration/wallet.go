package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/holiman/uint256"
	clt "github.com/mezonai/mmn/client"
	"github.com/mezonai/mmn/logx"

	"github.com/mr-tron/base58"
)

const WALLETS_DIR = "wallets"

func ToBigNumber(s int64) *uint256.Int {
	scale := new(uint256.Int).Exp(uint256.NewInt(10), uint256.NewInt(clt.NATIVE_DECIMAL))
	return new(uint256.Int).Mul(uint256.NewInt(uint64(s)), scale)
}

func GetFaucetAccount(faucetPrivateKeyHex string) (string, ed25519.PrivateKey) {
	faucetPrivateKeyDer, err := hex.DecodeString(faucetPrivateKeyHex)
	if err != nil {
		logx.Error("WALLET", fmt.Sprintf("Decode faucet private key failed: %v", err))
		return "", nil
	}

	// Extract the last 32 bytes as the Ed25519 seed
	faucetSeed := faucetPrivateKeyDer[len(faucetPrivateKeyDer)-32:]
	faucetPrivateKey := ed25519.NewKeyFromSeed(faucetSeed)
	faucetPublicKey := faucetPrivateKey.Public().(ed25519.PublicKey)
	faucetPublicKeyBase58 := base58.Encode(faucetPublicKey[:])
	return faucetPublicKeyBase58, faucetPrivateKey
}

func CreateAndFundSenderWallet(client *clt.MmnClient, faucetPrivateKeyHex string, totalAmount int64) error {
	logx.Info("WALLET", "Creating sender wallet")

	seed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(seed)
	if err != nil {
		return fmt.Errorf("failed to generate seed: %v", err)
	}

	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	walletAddress := base58.Encode(publicKey)
	logx.Info("WALLET", fmt.Sprintf("💰 Generated sender wallet - Address: %s", walletAddress))

	// Create wallets directory if it doesn't exist
	if err := os.MkdirAll(WALLETS_DIR, 0755); err != nil {
		return fmt.Errorf("failed to create wallets directory: %v", err)
	}

	// Save private key to file (filename = address, content = privatekey in hex)
	filePath := filepath.Join(WALLETS_DIR, walletAddress)
	privateKeyHex := fmt.Sprintf("%x", privateKey)

	if err := os.WriteFile(filePath, []byte(privateKeyHex), 0600); err != nil {
		return fmt.Errorf("failed to save sender wallet to file: %v", err)
	}

	logx.Info("WALLET", fmt.Sprintf("Saved sender wallet to file: %s", filePath))

	logx.Info("WALLET", "Starting to transfer tokens to sender wallet")
	transferAmount := ToBigNumber(totalAmount)
	logx.Info("WALLET", fmt.Sprintf("Expected balance: %s", transferAmount.String()))

	faucetAddress, faucetPrivateKey := GetFaucetAccount(faucetPrivateKeyHex)
	ctx := context.Background()
	nonce, err := client.GetCurrentNonce(ctx, faucetAddress, "pending")
	if err != nil {
		return fmt.Errorf("failed to get faucet nonce: %v", err)
	}
	nextNonce := nonce + 1
	textData := "Funding migration sender wallet"
	err = TransferTokens(client, ctx, faucetAddress, walletAddress, transferAmount, faucetPrivateKey, nextNonce, textData)
	if err != nil {
		return fmt.Errorf("failed to transfer tokens to migration wallet: %v", err)
	}

	logx.Info("WALLET", "Successfully transferred tokens to migration wallet")

	// Retry logic for GetAccountByAddress
	var migrationAccount clt.Account
	maxRetries := 3
	retryDelay := 2 * time.Second
	time.Sleep(retryDelay)

	for i := 0; i < maxRetries; i++ {
		migrationAccount, err = GetAccountByAddress(client, ctx, walletAddress)
		if err == nil {
			break // Success, exit retry loop
		}

		if i < maxRetries-1 { // Don't sleep on last attempt
			logx.Warn("WALLET", fmt.Sprintf("Attempt %d/%d failed to get blockchain account for %s: %v. Retrying in %v...", i+1, maxRetries, walletAddress, err, retryDelay))
			time.Sleep(retryDelay)
		}
	}

	if err != nil {
		return fmt.Errorf("failed to get blockchain account for account %s after %d attempts: %v", walletAddress, maxRetries, err)
	}

	logx.Info("WALLET", fmt.Sprintf("Migration wallet creation completed successfully. Account %s balance: %s tokens, nonce: %d", walletAddress, migrationAccount.Balance, migrationAccount.Nonce))
	return nil
}

func GetMigrationWalletFromFile() (string, ed25519.PrivateKey, error) {
	// Check if wallets directory exists
	if _, err := os.Stat(WALLETS_DIR); os.IsNotExist(err) {
		return "", nil, fmt.Errorf("wallets directory does not exist, please create migration wallet first")
	}

	// Find any wallet file in the directory (assuming there's only one migration wallet)
	var walletFile string
	err := filepath.WalkDir(WALLETS_DIR, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			walletFile = path
			return filepath.SkipAll // Stop after finding the first file
		}
		return nil
	})

	if err != nil {
		return "", nil, fmt.Errorf("failed to search for wallet files: %v", err)
	}

	if walletFile == "" {
		return "", nil, fmt.Errorf("no wallet file found, please create migration wallet first")
	} // Extract address from filename
	address := filepath.Base(walletFile)

	// Read private key from file
	privateKeyHex, err := os.ReadFile(walletFile)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read wallet file %s: %v", walletFile, err)
	}

	// Convert hex string to private key
	privateKeyBytes, err := hex.DecodeString(string(privateKeyHex))
	if err != nil {
		return "", nil, fmt.Errorf("failed to decode private key hex: %v", err)
	}

	if len(privateKeyBytes) != ed25519.PrivateKeySize {
		return "", nil, fmt.Errorf("invalid private key length: expected %d, got %d", ed25519.PrivateKeySize, len(privateKeyBytes))
	}

	privateKey := ed25519.PrivateKey(privateKeyBytes)

	// Verify address matches the private key
	publicKey := privateKey.Public().(ed25519.PublicKey)
	expectedAddress := base58.Encode(publicKey[:])

	if address != expectedAddress {
		return "", nil, fmt.Errorf("address mismatch: filename=%s, derived=%s", address, expectedAddress)
	}

	return address, privateKey, nil
}

func MapUsersWalletAddress(users []map[string]interface{}) []map[string]interface{} {
	for _, user := range users {
		userId := user["id"].(int64)
		user["address"] = clt.GenerateAddress(strconv.FormatInt(userId, 10))
	}
	return users
}
