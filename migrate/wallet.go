package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	mmnClient "github.com/mezonai/mmn/client"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/utils"
	"github.com/mr-tron/base58"
)

var ErrKeyNotFound = errors.New("keystore: not found")

type WalletManager interface {
	LoadKey(userID uint64) (addr string, privKey []byte, err error)
	CreateKey(userID uint64, isSave bool) (addr string, privKey []byte, err error)
}
type pgStore struct {
	db   *sql.DB
	aead cipher.AEAD
}

// getFaucetAccount returns the hardcoded faucet account for testing
func GetFaucetAccount() (string, ed25519.PrivateKey) {
	faucetPrivateKeyHex := "302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee"
	faucetPrivateKeyDer, err := hex.DecodeString(faucetPrivateKeyHex)
	if err != nil {
		fmt.Println("err", err)
		panic(err)
	}

	// Extract the last 32 bytes as the Ed25519 seed
	faucetSeed := faucetPrivateKeyDer[len(faucetPrivateKeyDer)-32:]
	faucetPrivateKey := ed25519.NewKeyFromSeed(faucetSeed)
	faucetPublicKey := faucetPrivateKey.Public().(ed25519.PublicKey)
	faucetPublicKeyBase58 := base58.Encode(faucetPublicKey[:])
	return faucetPublicKeyBase58, faucetPrivateKey
}

func NewPgEncryptedStore(db *sql.DB, base64MasterKey string) (WalletManager, error) {
	mk, err := base64.StdEncoding.DecodeString(base64MasterKey)
	if err != nil {
		return nil, fmt.Errorf("master-key decode: %w", err)
	}
	if len(mk) != 32 {
		return nil, errors.New("master-key must be 32 bytes")
	}

	block, _ := aes.NewCipher(mk)
	aead, _ := cipher.NewGCM(block)

	return &pgStore{db: db, aead: aead}, nil
}

// ---------- helpers ----------
func (p *pgStore) encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return append(nonce, p.aead.Seal(nil, nonce, plain, nil)...), nil
}
func (p *pgStore) decrypt(ciphertext []byte) ([]byte, error) {
	ns := p.aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("ciphertext too short")
	}
	return p.aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}

// ---------- WalletManager ----------
func (p *pgStore) LoadKey(uid uint64) (string, []byte, error) {
	var addr string
	var enc []byte

	// TODO: need use exist db model
	err := p.db.QueryRow(`SELECT address, enc_privkey FROM mmn_user_keys WHERE user_id=$1`, uid).
		Scan(&addr, &enc)
	if errors.Is(err, sql.ErrNoRows) {
		fmt.Printf("LoadKey ErrNoRows %d %s %s %v\n", uid, addr, enc, err)
		return "", nil, mmnClient.ErrKeyNotFound
	}
	if err != nil {
		fmt.Printf("LoadKey Err %d %s %s %v\n", uid, addr, enc, err)
		return "", nil, err
	}

	priv, err := p.decrypt(enc)
	return addr, priv, err
}

func (p *pgStore) CreateKey(uid uint64, isSave bool) (string, []byte, error) {
	fmt.Printf("CreateKey start %d\n", uid)

	// Generate Ed25519 seed (32 bytes)
	seed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(seed)
	if err != nil {
		return "", nil, err
	}

	// Generate Ed25519 key pair from seed
	privKey := ed25519.NewKeyFromSeed(seed)
	pubKey := privKey.Public().(ed25519.PublicKey)

	// Address is base58-encoded public key (to match node format)
	addr := base58.Encode(pubKey)

	// Store the seed (not the full private key) for SignTx compatibility
	enc, err := p.encrypt(seed)
	if err != nil {
		return "", nil, err
	}

	if isSave {
		_, err = p.db.Exec(
			`INSERT INTO mmn_user_keys(user_id,address,enc_privkey) VALUES($1,$2,$3)`,
			uid, addr, enc,
		)
	}

	fmt.Printf("CreateKey done %d %s\n", uid, addr)
	return addr, seed, err
}

// CreateMigrationWallet creates a new wallet for migration using CreateKey and saves it to file
func CreateMigrationWallet(db *sql.DB, config *Config, faucetAddress string, faucetPrivateKey ed25519.PrivateKey, dryRun bool) error {
	LogInfo("🔧 Starting migration wallet creation")

	if dryRun {
		LogInfo("🔍 DRY-RUN: Would create new migration wallet and save to file")
		return nil
	}

	// Step 1: Create wallet using CreateKey and save to file
	// Create wallet manager
	ks, err := NewPgEncryptedStore(db, config.MasterKey)
	if err != nil {
		return fmt.Errorf("failed to create wallet manager: %v", err)
	}

	// Use a special userID for migration wallet (use max int64 to avoid conflicts)
	const migrationUserID = uint64(0) // Special high ID for migration wallet

	// Create new wallet using CreateKey
	walletAddress, seed, err := ks.CreateKey(migrationUserID, false)
	if err != nil {
		return fmt.Errorf("failed to create migration wallet: %v", err)
	}

	LogInfo("💰 Generated migration wallet - Address: %s", walletAddress)

	// Create wallets directory if it doesn't exist
	walletsDir := "wallets"
	if err := os.MkdirAll(walletsDir, 0755); err != nil {
		return fmt.Errorf("failed to create wallets directory: %v", err)
	}

	// Create full private key from seed for file storage
	privateKey := ed25519.NewKeyFromSeed(seed)

	// Save private key to file (filename = address, content = privatekey in hex)
	filename := filepath.Join(walletsDir, walletAddress)
	privateKeyHex := fmt.Sprintf("%x", privateKey)

	if err := os.WriteFile(filename, []byte(privateKeyHex), 0600); err != nil {
		return fmt.Errorf("failed to save migration wallet to file: %v", err)
	}

	LogInfo("💾 Saved migration wallet to file: %s", filename)

	// Step 2: Transfer tokens from faucet to migration wallet
	// Calculate total wallet balance of all users
	totalUsersWallet, err := GetTotalUsersWallet(db)
	if err != nil {
		return fmt.Errorf("failed to calculate total users wallet: %v", err)
	}
	LogInfo("📊 Total users wallet balance: %d", totalUsersWallet)

	transferAmount := utils.ToBigNumber(totalUsersWallet)

	LogInfo("📊 Balance comparison:")
	LogInfo("   Expected balance: %s", transferAmount.String())

	// Need to transfer more tokens
	LogInfo("💸 Transferring %s tokens from faucet to migration wallet", transferAmount.String())

	err = TransferTokens(faucetAddress, walletAddress, transferAmount, faucetPrivateKey)
	if err != nil {
		return fmt.Errorf("failed to transfer tokens to migration wallet: %v", err)
	}
	LogInfo("✅ Successfully transferred tokens to migration wallet")

	time.Sleep(2 * time.Second)
	migrationAccount, err := GetAccountByAddress(walletAddress)
	if err != nil {
		LogWarn("⚠️ Could not get blockchain account for account %s: %v", walletAddress, err)
	}

	LogInfo("🎉 Migration wallet creation completed successfully. Account %s balance: %s tokens, nonce: %d", walletAddress, migrationAccount.Balance, migrationAccount.Nonce)

	return nil
}

// GetMigrationWalletFromFile reads migration wallet from file
func GetMigrationWalletFromFile() (string, ed25519.PrivateKey, error) {
	walletsDir := "wallets"

	// Check if wallets directory exists
	if _, err := os.Stat(walletsDir); os.IsNotExist(err) {
		return "", nil, fmt.Errorf("wallets directory does not exist, please create migration wallet first")
	}

	// Find any wallet file in the directory (assuming there's only one migration wallet)
	var walletFile string
	err := filepath.WalkDir(walletsDir, func(path string, d fs.DirEntry, err error) error {
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
