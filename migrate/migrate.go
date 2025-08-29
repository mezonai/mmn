package main

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/utils"
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
		balance := user["balance"].(int64)
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

		ks, err := NewPgEncryptedStore(db, config.MasterKey)
		if err != nil {
			fmt.Printf("Failed to create wallet manager: %v", err)
			continue
		}
		address, _, err := ks.LoadKey(uint64(userID))

		if !*dryRun {
			if err != nil {
				LogDebug("Create wallet for user %d\n", userID)
				// Create new wallet
				if address, _, err = ks.CreateKey(uint64(userID)); err != nil {
					continue
				}
				LogWalletCreated(userID, address)
			}
			LogDatabaseOperation("INSERT/UPDATE", "mmn_user_keys", 1)
			LogDebug("💾 Saved wallet to database for user %d", userID)

			// Transfer tokens from faucet to user wallet
			err = TransferTokens(config.MMNEndpoint, faucetAddress, address, utils.ToBigNumber(balance), faucetPrivateKey) // Transfer 1000 tokens
			if err != nil {
				LogError("❌ Failed to transfer tokens to user %d: %v", userID, err)
				// Continue even if transfer fails, wallet is already created
			} else {
				LogTokenTransfer(faucetAddress, address, balance)
			}

			// Add delay between transactions to avoid nonce conflicts
			time.Sleep(2 * time.Second)
		} else {
			LogInfo("🔍 DRY-RUN: Would create wallet and transfer tokens for user %d (%s)", userID, name)
			LogDebug("    Wallet Address: %s", address)
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
