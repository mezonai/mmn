package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/holiman/uint256"
	_ "github.com/lib/pq"
	clt "github.com/mezonai/mmn/client"
	"github.com/mezonai/mmn/logx"
	"gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	fmt.Println("Starting migration.........")
	// Parse command line flags
	createSenderWallet := flag.Bool("create-sender-wallet", false, "Create sender wallet")
	runMigration := flag.Bool("run-migration", false, "Run migration")
	mmnEndpoint := flag.String("mmn-endpoint", "", "MMN endpoint")
	usersDataFilePath := flag.String("users-data-file", "", "Path file .csv user data")
	privateKeyFile := flag.String("faucet-private-key-file", "", "faucet private key file")
	privateKey := flag.String("faucet-private-key", "", "faucet private key in hex")
	flag.Parse()

	lumberjackLogger := &lumberjack.Logger{
		Filename: "migration.log",
	}
	logx.InitWithOutput(lumberjackLogger)

	if *mmnEndpoint == "" {
		logx.Error("CONFIG", "MMN endpoint is required")
		return
	}

	if *usersDataFilePath == "" {
		logx.Error("CONFIG", "Users data file path is required")
		return
	}

	logx.Info("CONFIG", fmt.Sprintf("Configuration loaded - MMN Endpoint: %s", *mmnEndpoint))

	users, err := GetUsers(*usersDataFilePath)
	if err != nil {
		logx.Error("USERS", fmt.Sprintf("failed to get all users: %v", err))
		return
	}

	client, err := NewMmnClient(*mmnEndpoint)
	if err != nil {
		logx.Error("CONFIG", fmt.Sprintf("Failed to create mmn client: %v", err))
		return
	}
	defer client.Close()

	if *createSenderWallet {
		faucetPrivateKey, err := loadSenderPrivateKey(*privateKey, *privateKeyFile)
		if err != nil {
			logx.Error("CONFIG", fmt.Sprintf("Failed to load faucet private key: %v", err))
			return
		}
		totalUsersAmount := calculateTotalUserAmount(users)
		err = CreateAndFundSenderWallet(client, faucetPrivateKey, totalUsersAmount)
		if err != nil {
			logx.Error("WALLET", fmt.Sprintf("Failed to create sender wallet: %v", err))
		}
	}

	if *runMigration {
		runUserMigration(client, users)
	}
	fmt.Println("Migration completed!!!!!!!!!!!!")
}

// runUserMigration handles the user migration process
func runUserMigration(client *clt.MmnClient, users []map[string]interface{}) {
	logx.Info("MIGRATION", fmt.Sprintf("Starting migration process for %d users", len(users)))

	// Get migration wallet info for transfers from file
	migrationAddress, migrationPrivateKey, err := GetMigrationWalletFromFile()
	if err != nil {
		logx.Error("MIGRATION", fmt.Sprintf("Failed to get migration wallet from file: %v", err))
	}
	logx.Info("MIGRATION", fmt.Sprintf("Migration wallet loaded from file - Address: %s", migrationAddress))
	users = MapUsersWalletAddress(users)
	var processed, successful int
	for _, user := range users {
		userID := user["id"].(int64)
		name := user["name"].(string)
		expectedBalance := user["balance"].(int64)
		userAddress := user["address"].(string)
		processed++
		logx.Info("MIGRATION", fmt.Sprintf("Processing user %d (%s), wallet: %s", userID, name, userAddress))
		logx.Info("MIGRATION", fmt.Sprintf("Progress: %d/%d users processed", processed, len(users)))

		if userAddress != "" {
			// Check current balance on blockchain
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			account, err := GetAccountByAddress(client, ctx, userAddress)
			if err != nil {
				logx.Error("MIGRATION", fmt.Sprintf("Could not get blockchain account for user %d: %v", userID, err))
				logx.Info("MIGRATION", "Assuming user balance is 0, will transfer full amount")
				// Create a zero balance account
				account = clt.Account{Balance: uint256.NewInt(0)}
			}

			currentBalance := account.Balance
			expectedBalanceBig := ToBigNumber(expectedBalance)

			logx.Info("MIGRATION", fmt.Sprintf("User %d Balance comparison:", userID))
			logx.Info("MIGRATION", fmt.Sprintf("Database balance: %d", expectedBalance))
			logx.Info("MIGRATION", fmt.Sprintf("Blockchain balance: %s", currentBalance.String()))

			// Calculate transfer amount needed
			if currentBalance.Cmp(expectedBalanceBig) < 0 {
				transferAmount := new(uint256.Int).Sub(expectedBalanceBig, currentBalance)
				logx.Info("MIGRATION", fmt.Sprintf("Transferring %s tokens from migration wallet to user %d", transferAmount.String(), userID))
				nonce, err := client.GetCurrentNonce(ctx, migrationAddress, "pending")
				if err != nil {
					logx.Error("MIGRATION", fmt.Sprintf("Failed to get migration wallet nonce: %v", err))
					continue
				}
				nextNonce := nonce + 1
				err = TransferTokens(client, ctx, migrationAddress, userAddress, transferAmount, migrationPrivateKey, nextNonce, fmt.Sprintf("Transfer tokens to user %d", userID))
				if err != nil {
					logx.Error("MIGRATION", fmt.Sprintf("Failed to transfer tokens to user %d: %v", userID, err))
					continue
				} else {
					logx.Info("MIGRATION", fmt.Sprintf("Token transferred %d from migration wallet to user %d", transferAmount.Uint64(), userID))
				}
				time.Sleep(2 * time.Second)
			} else if currentBalance.Cmp(expectedBalanceBig) > 0 {
				logx.Error("MIGRATION", fmt.Sprintf("User %d blockchain balance (%s) is higher than database balance (%d)",
					userID, currentBalance.String(), expectedBalance))
				logx.Info("MIGRATION", "No transfer needed - user has sufficient balance")
			} else {
				logx.Info("MIGRATION", fmt.Sprintf("User %d balance is already synchronized", userID))
			}
		}
		successful++
		logx.Info("MIGRATION", fmt.Sprintf("Successfully processed user %d", userID))
	}

	// Final summary
	logx.Info("MIGRATION", fmt.Sprintf("Migration completed: %d/%d users processed successfully", successful, processed))
	logx.Info("MIGRATION", "Migration Summary:")
	logx.Info("MIGRATION", fmt.Sprintf("Total users: %d", len(users)))
	logx.Info("MIGRATION", fmt.Sprintf("Processed: %d", processed))
	logx.Info("MIGRATION", fmt.Sprintf("Successful: %d", successful))
	if processed > successful {
		logx.Info("MIGRATION", fmt.Sprintf("Failed: %d", processed-successful))
	}
}

func loadSenderPrivateKey(privateKey string, privateKeyFile string) (string, error) {
	if privateKey != "" {
		return privateKey, nil
	}
	bytes, err := os.ReadFile(privateKeyFile)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func calculateTotalUserAmount(users []map[string]interface{}) int64 {
	var total int64
	for _, user := range users {
		if balance, ok := user["balance"].(int64); ok {
			total += balance
		}
	}
	return total
}
