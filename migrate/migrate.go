package main

import (
	"flag"
	"log"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// parseLogLevel converts string log level to LogLevel enum
func parseLogLevel(level string) LogLevel {
	switch strings.ToLower(level) {
	case "debug":
		return DEBUG
	case "info":
		return INFO
	case "warn", "warning":
		return WARN
	case "error":
		return ERROR
	case "fatal":
		return FATAL
	default:
		return INFO
	}
}

func main() {
	// Parse command line flags
	dryRun := flag.Bool("dry-run", false, "Run in dry-run mode (no actual changes)")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error, fatal)")
	flag.Parse()

	// Initialize logger
	level := parseLogLevel(*logLevel)
	InitLogger(level)

	LogInfo("🚀 Starting MMN Migration Tool (dry-run: %v, log-level: %s)", *dryRun, *logLevel)

	// Load configuration
	config := LoadConfig()
	LogInfo("📋 Configuration loaded - MMN Endpoint: %s", config.MMNEndpoint)

	// Connect to database
	db, err := ConnectDatabase(config.DatabaseURL)
	if err != nil {
		LogFatal("❌ Database connection failed: %v", err)
	}
	defer db.Close()

	// Test MMN blockchain connection
	if err := TestConnection(config.MMNEndpoint); err != nil {
		LogWarn("⚠️ MMN blockchain connection test failed: %v", err)
		LogWarn("⚠️ Continuing anyway - transfers will fail if blockchain is unavailable")
	} else {
		LogConnectionTest("MMN blockchain", config.MMNEndpoint, true)
	}

	// Create mmn_user_keys table if it doesn't exist
	if err := CreateUserKeysTable(db); err != nil {
		log.Fatalf("Failed to create mmn_user_keys table: %v", err)
	}

	// Get all users from database
	users, err := GetUsers(db)
	if err != nil {
		LogFatal("❌ Failed to get users: %v", err)
	}

	LogMigrationStart(len(users))

	// Check for existing wallets
	existingWallets, err := CountExistingWallets(db)
	if err != nil {
		LogWarn("⚠️ Could not count existing wallets: %v", err)
		existingWallets = 0
	}
	LogInfo("📊 Found %d existing wallets", existingWallets)

	// Create faucet account
	faucetAddress, faucetPrivateKey := GetFaucetAccount()
	if err != nil {
		LogFatal("❌ Failed to create faucet account: %v", err)
	}
	LogInfo("✅ Faucet account ready - Address: %s", faucetAddress)

	// Process each user
	var processed, successful int
	for _, user := range users {
		userID := user["id"].(int)
		name := user["name"].(string)
		balance := user["balance"].(uint64)
		processed++
		LogUserProcessing(userID, name)
		LogDebug("Progress: %d/%d users processed", processed, len(users))

		// Check if wallet already exists for this user
		exists, err := CheckExistingWallet(db, userID)
		if err != nil {
			log.Printf("❌ Error checking existing wallet for user %d: %v", userID, err)
			continue
		}

		if exists {
			log.Printf("⏭️  User %d (%s) already has a wallet, skipping", userID, name)
			continue
		}

		// Create new wallet
		wallet, err := CreateWallet()
		if err != nil {
			LogError("❌ Failed to create wallet for user %d: %v", userID, err)
			continue
		}
		LogWalletCreated(userID, wallet.Address)

		// Encrypt private key using AES-GCM
		encryptedPrivateKey, err := EncryptPrivateKey(wallet.PrivateKey, config.MasterKey)
		if err != nil {
			LogError("❌ Failed to encrypt private key for user %d: %v", userID, err)
			continue
		}
		LogDebug("🔐 Private key encrypted for user %d", userID)

		if !*dryRun {
			// Save wallet to database
			if err := SaveWallet(db, userID, wallet, encryptedPrivateKey); err != nil {
				LogError("❌ Failed to save wallet for user %d: %v", userID, err)
				continue
			}
			LogDatabaseOperation("INSERT/UPDATE", "mmn_user_keys", 1)
			LogDebug("💾 Saved wallet to database for user %d", userID)

			// Transfer tokens from faucet to user wallet
			err = TransferTokens(config.MMNEndpoint, faucetAddress, wallet.Address, balance, faucetPrivateKey) // Transfer 1000 tokens
			if err != nil {
				LogError("❌ Failed to transfer tokens to user %d: %v", userID, err)
				// Continue even if transfer fails, wallet is already created
			} else {
				LogTokenTransfer(faucetAddress, wallet.Address, balance)
			}

			// Add delay between transactions to avoid nonce conflicts
			time.Sleep(2 * time.Second)
		} else {
			LogInfo("🔍 DRY-RUN: Would create wallet and transfer tokens for user %d (%s)", userID, name)
			LogDebug("    Wallet Address: %s", wallet.Address)
			LogDebug("    Public Key: %x", wallet.PublicKey)
		}

		successful++
		LogDebug("✅ Successfully processed user %d", userID)
	}

	// Final summary
	LogMigrationComplete(processed, successful)
	LogInfo("📊 Migration Summary:")
	LogInfo("   Total users: %d", len(users))
	LogInfo("   Processed: %d", processed)
	LogInfo("   Successful: %d", successful)
	if processed > successful {
		LogWarn("   Failed: %d", processed-successful)
	}
}
