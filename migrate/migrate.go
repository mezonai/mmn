package main

import (
	"database/sql"
	"flag"
	"log"
	"strings"
	"time"

	"github.com/holiman/uint256"
	_ "github.com/lib/pq"
	clt "github.com/mezonai/mmn/client"
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
	runHRM := flag.Bool("hrm", false, "Run HRM setup only")
	runMigration := flag.Bool("migrate", false, "Run user migration only")
	flag.Parse()

	// Initialize logger
	level := parseLogLevel(*logLevel)
	InitLogger(level)

	// Determine what to run
	if !*runHRM && !*runMigration {
		// Default: run both HRM and migration
		*runHRM = true
		*runMigration = true
	}

	if *runHRM {
		LogInfo("🚀 Starting HRM Setup Tool (dry-run: %v, log-level: %s)", *dryRun, *logLevel)
	}
	if *runMigration {
		LogInfo("🚀 Starting MMN Migration Tool (dry-run: %v, log-level: %s)", *dryRun, *logLevel)
	}

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

	// Execute HRM setup if requested
	if *runHRM {
		// Create faucet account
		faucetAddress, faucetPrivateKey := GetFaucetAccount()
		if err != nil {
			LogFatal("❌ Failed to create faucet account: %v", err)
		}
		LogInfo("✅ Faucet account ready - Address: %s", faucetAddress)

		LogInfo("🔧 Starting HRM setup process...")
		err = CreateHRM(db, config, faucetAddress, faucetPrivateKey, *dryRun)
		if err != nil {
			LogFatal("❌ HRM setup failed: %v", err)
		}
		LogInfo("✅ HRM setup completed successfully")
	}

	// Execute migration if requested
	if *runMigration {
		runUserMigration(db, config, *dryRun)
	}
}

// runUserMigration handles the user migration process
func runUserMigration(db *sql.DB, config *Config, dryRun bool) {
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

	// Get HRM wallet info for transfers
	hrmAddress, hrmPrivateKey, err := GetHRMAccount(db, config)
	if err != nil {
		LogFatal("❌ Failed to get HRM account: %v", err)
	}
	LogInfo("✅ HRM wallet ready for transfers - Address: %s", hrmAddress)

	var processed, successful int
	for _, user := range users {
		userID := user["id"].(int)
		name := user["name"].(string)
		expectedBalance := user["balance"].(int64)
		processed++
		LogUserProcessing(userID, name)
		LogDebug("Progress: %d/%d users processed", processed, len(users))

		// Check if wallet already exists for this user
		exists, err := CheckExistingWallet(db, userID)
		if err != nil {
			log.Printf("❌ Error checking existing wallet for user %d: %v", userID, err)
			continue
		}

		var userAddress string

		if !exists {
			// Create new wallet for user
			if dryRun {
				LogInfo("🔍 DRY-RUN: Would create wallet for user %d (%s)", userID, name)
			} else {
				ks, err := NewPgEncryptedStore(db, config.MasterKey)
				if err != nil {
					LogError("❌ Failed to create wallet manager for user %d: %v", userID, err)
					continue
				}

				userAddress, _, err = ks.CreateKey(uint64(userID))
				if err != nil {
					LogError("❌ Failed to create wallet for user %d: %v", userID, err)
					continue
				}
				LogWalletCreated(userID, userAddress)
				LogDatabaseOperation("INSERT", "mmn_user_keys", 1)
			}
		} else {
			// Get existing wallet address
			userAddress, err = GetUserWalletAddress(db, userID)
			if err != nil {
				LogError("❌ Failed to get wallet address for user %d: %v", userID, err)
				continue
			}
			LogInfo("ℹ️ User %d (%s) already has wallet: %s", userID, name, userAddress)
		}

		if !dryRun && userAddress != "" {
			// Check current balance on blockchain
			account, err := GetAccountByAddress(userAddress)
			if err != nil {
				LogWarn("⚠️ Could not get blockchain account for user %d: %v", userID, err)
				LogInfo("🔄 Assuming user balance is 0, will transfer full amount")
				// Create a zero balance account
				account = clt.Account{Balance: uint256.NewInt(0)}
			}

			currentBalance := account.Balance
			expectedBalanceBig := utils.ToBigNumber(expectedBalance)

			LogInfo("� User %d Balance comparison:", userID)
			LogInfo("   Database balance: %d", expectedBalance)
			LogInfo("   Blockchain balance: %s", currentBalance.String())

			// Calculate transfer amount needed
			if currentBalance.Cmp(expectedBalanceBig) < 0 {
				transferAmount := new(uint256.Int).Sub(expectedBalanceBig, currentBalance)
				LogInfo("💸 Transferring %s tokens from HRM to user %d", transferAmount.String(), userID)

				err = TransferTokens(config.MMNEndpoint, hrmAddress, userAddress, transferAmount, hrmPrivateKey)
				if err != nil {
					LogError("❌ Failed to transfer tokens to user %d: %v", userID, err)
					continue
				} else {
					LogTokenTransfer(hrmAddress, userAddress, int64(transferAmount.Uint64()))
				}
			} else if currentBalance.Cmp(expectedBalanceBig) > 0 {
				LogWarn("⚠️ User %d blockchain balance (%s) is higher than database balance (%d)",
					userID, currentBalance.String(), expectedBalance)
				LogInfo("ℹ️ No transfer needed - user has sufficient balance")
			} else {
				LogInfo("✅ User %d balance is already synchronized", userID)
			}

			// Add delay between transactions to avoid nonce conflicts
			time.Sleep(2 * time.Second)
		} else if dryRun {
			LogInfo("🔍 DRY-RUN: Would check and sync balance for user %d (%s)", userID, name)
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
