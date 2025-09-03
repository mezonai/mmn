package main

import (
	"crypto/ed25519"
	"database/sql"
	"fmt"

	"github.com/holiman/uint256"
	clt "github.com/mezonai/mmn/client"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/utils"
)

// CreateHRM handles the HRM user creation and wallet setup
func CreateHRM(db *sql.DB, config *Config, faucetAddress string, faucetPrivateKey ed25519.PrivateKey, dryRun bool) error {
	LogInfo("🔧 Starting HRM setup process")

	// Calculate total wallet balance of all users (excluding HRM)
	totalUsersWallet, err := GetTotalUsersWallet(db)
	if err != nil {
		return fmt.Errorf("failed to calculate total users wallet: %v", err)
	}
	LogInfo("� Total users wallet balance: %d", totalUsersWallet)

	// Step 1: Check if HRM user exists
	userExists, userID, dbWalletBalance, err := CheckUserExists(db, HRM_USERNAME)
	if err != nil {
		return fmt.Errorf("failed to check HRM user existence: %v", err)
	}

	if !userExists {
		if dryRun {
			LogInfo("🔍 DRY-RUN: Would create HRM user with balance %d", totalUsersWallet)
		} else {
			// Create HRM user with wallet balance equal to total users wallet
			userID, err = CreateUser(db, HRM_USERNAME, totalUsersWallet)
			if err != nil {
				return fmt.Errorf("failed to create HRM user: %v", err)
			}
			dbWalletBalance = totalUsersWallet
			LogInfo("✅ Created HRM user with ID %d and balance %d", userID, totalUsersWallet)
		}
	} else {
		LogInfo("ℹ️ HRM user already exists with ID %d and balance %d", userID, dbWalletBalance)

		// Update HRM user balance to match total users wallet if different
		if dbWalletBalance != totalUsersWallet {
			if dryRun {
				LogInfo("🔍 DRY-RUN: Would update HRM balance from %d to %d", dbWalletBalance, totalUsersWallet)
			} else {
				_, err = db.Exec("UPDATE users SET wallet = $1 WHERE name = $2", totalUsersWallet, HRM_USERNAME)
				if err != nil {
					return fmt.Errorf("failed to update HRM user balance: %v", err)
				}
				dbWalletBalance = totalUsersWallet
				LogInfo("✅ Updated HRM user balance to %d (total users wallet)", totalUsersWallet)
			}
		} else {
			LogInfo("✅ HRM user balance already matches total users wallet (%d)", totalUsersWallet)
		}
	}

	if dryRun && !userExists {
		LogInfo("🔍 DRY-RUN: HRM setup completed (simulation)")
		return nil
	}

	// Step 2: Check if HRM user has a wallet
	walletAddress, err := GetUserWalletAddress(db, userID)
	if err != nil {
		return fmt.Errorf("failed to get HRM wallet address: %v", err)
	}

	if walletAddress == "" {
		if dryRun {
			LogInfo("🔍 DRY-RUN: Would create wallet for HRM user")
		} else {
			// Create wallet for HRM user
			ks, err := NewPgEncryptedStore(db, config.MasterKey)
			if err != nil {
				return fmt.Errorf("failed to create wallet manager: %v", err)
			}

			walletAddress, _, err = ks.CreateKey(uint64(userID))
			if err != nil {
				return fmt.Errorf("failed to create wallet for HRM user: %v", err)
			}
			LogInfo("💰 Created wallet for HRM user - Address: %s", walletAddress)
		}
	} else {
		LogInfo("ℹ️ HRM user already has wallet: %s", walletAddress)
	}

	if dryRun {
		LogInfo("🔍 DRY-RUN: Would check and sync HRM wallet balance")
		LogInfo("🔍 DRY-RUN: HRM setup completed (simulation)")
		return nil
	}

	// Step 3: Check wallet balance on blockchain vs database
	account, err := GetAccountByAddress(walletAddress)
	if err != nil {
		LogWarn("⚠️ Could not get blockchain account for HRM wallet: %v", err)
		LogInfo("🔄 Assuming blockchain balance is 0, will transfer from faucet")
		account = clt.Account{Balance: uint256.NewInt(0)}
	}

	blockchainBalance := account.Balance
	expectedBalance := utils.ToBigNumber(dbWalletBalance)

	LogInfo("📊 HRM Balance comparison:")
	LogInfo("   Database balance: %d", dbWalletBalance)
	LogInfo("   Blockchain balance: %s", blockchainBalance.String())
	LogInfo("   Expected balance: %s", expectedBalance.String())

	// If blockchain balance doesn't match database balance, transfer the difference
	if blockchainBalance.Cmp(expectedBalance) != 0 {
		if blockchainBalance.Cmp(expectedBalance) < 0 {
			// Need to transfer more tokens
			transferAmount := new(uint256.Int).Sub(expectedBalance, blockchainBalance)
			LogInfo("💸 Transferring %s tokens from faucet to HRM wallet", transferAmount.String())

			err = TransferTokens(config.MMNEndpoint, faucetAddress, walletAddress, transferAmount, faucetPrivateKey)
			if err != nil {
				return fmt.Errorf("failed to transfer tokens to HRM wallet: %v", err)
			}
			LogInfo("✅ Successfully transferred tokens to HRM wallet")
		} else {
			// Blockchain balance is higher than expected
			LogWarn("⚠️ HRM wallet blockchain balance (%s) is higher than database balance (%d)",
				blockchainBalance.String(), dbWalletBalance)
			LogInfo("ℹ️ No action needed - wallet has sufficient balance")
		}
	} else {
		LogInfo("✅ HRM wallet balance is already synchronized")
	}

	LogInfo("🎉 HRM setup completed successfully")
	return nil
}
