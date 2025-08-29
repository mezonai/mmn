package main

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/holiman/uint256"
	mmnClient "github.com/mezonai/mmn/client"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/keystore"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/service"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/utils"
	mmnpb "github.com/mezonai/mmn/proto"
	"github.com/mr-tron/base58"

	_ "github.com/lib/pq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Test configuration - set these via environment variables
const (
	defaultMainnetEndpoints = "localhost:9001" // Your local mainnet gRPC endpoint
	defaultDbURL            = "postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable"
	defaultMasterKey        = "bWV6b25fdGVzdF9tYXN0ZXJfa2V5XzEyMzQ1Njc4OTA=" // base64 of "mezon_test_master_key_1234567890"
)

type TestService struct {
	TxService      *service.TxService
	AccountService *service.AccountService
}

func setupIntegrationTest(t *testing.T) (*TestService, func()) {
	t.Helper()

	// Get config from environment or use defaults
	endpoint := getEnvOrDefault("MMN_ENDPOINT", defaultMainnetEndpoints)
	dbURL := getEnvOrDefault("DATABASE_URL", defaultDbURL)
	masterKey := getEnvOrDefault("MASTER_KEY", defaultMasterKey)

	fmt.Println("endpoint", endpoint)
	fmt.Println("dbURL", dbURL)
	fmt.Println("masterKey", masterKey)

	// Setup database connection
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// Test database connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	// Create MMN user keys table if not exists (for testing)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS mmn_user_keys (
			user_id      BIGINT PRIMARY KEY,
			address      VARCHAR(255) NOT NULL,
			enc_privkey  BYTEA NOT NULL,
			created_at   TIMESTAMPTZ DEFAULT now(),
			updated_at   TIMESTAMPTZ DEFAULT now()
		);
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY,
			username VARCHAR(255) NOT NULL
		);
		INSERT INTO users (id, username) VALUES (0, 'mezon_test_user_0'), (1, 'mezon_test_user_1'), (2, 'mezon_test_user_2') ON CONFLICT (id) DO NOTHING;
		CREATE TABLE IF NOT EXISTS unlocked_items (
			id SERIAL PRIMARY KEY,
			user_id int8 NOT NULL,
			item_id int8 NOT NULL,
			item_type int2 DEFAULT 0 NOT NULL,
			status int2 DEFAULT 0 NOT NULL,
			create_time timestamptz DEFAULT now() NOT NULL,
			update_time timestamptz DEFAULT now() NOT NULL,
			tx_hash varchar NULL,
			CONSTRAINT unlocked_items_user_id_item_id_key UNIQUE (user_id, item_id)
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create mmn_user_keys table: %v", err)
	}

	// Setup keystore with encryption
	walletManager, err := keystore.NewPgEncryptedStore(db, masterKey)
	if err != nil {
		t.Fatalf("Failed to create wallet manager: %v", err)
	}

	// Setup mainnet client
	config := mmnClient.Config{
		Endpoint: endpoint,
	}

	mainnetClient, err := mmnClient.NewClient(config)
	if err != nil {
		t.Fatalf("Failed to create mainnet client: %v", err)
	}

	// Create TxService with real implementations
	txService := service.NewTxService(mainnetClient, walletManager, db)
	accountService := service.NewAccountService(mainnetClient, walletManager, db)

	// Cleanup function
	cleanup := func() {
		db.Close()
	}

	return &TestService{
		TxService:      txService,
		AccountService: accountService,
	}, cleanup
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getFaucetAccount() (string, ed25519.PrivateKey) {
	fmt.Println("getFaucetAccount")
	faucetPrivateKeyHex := "302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee"
	faucetPrivateKeyDer, err := hex.DecodeString(faucetPrivateKeyHex)
	if err != nil {
		fmt.Println("err", err)
		panic(err)
	}
	fmt.Println("faucetPrivateKeyDer")

	// Extract the last 32 bytes as the Ed25519 seed
	faucetSeed := faucetPrivateKeyDer[len(faucetPrivateKeyDer)-32:]
	faucetPrivateKey := ed25519.NewKeyFromSeed(faucetSeed)
	faucetPublicKey := faucetPrivateKey.Public().(ed25519.PublicKey)
	faucetPublicKeyBase58 := base58.Encode(faucetPublicKey[:])
	fmt.Println("faucetPublicKeyBase58", faucetPublicKeyBase58)
	return faucetPublicKeyBase58, faucetPrivateKey
}

// seedAccountFromFaucet sends initial tokens from faucet to a given address
func seedAccountFromFaucet(t *testing.T, ctx context.Context, service *TestService, toAddress string, amount *uint256.Int) (string, error) {
	faucetPublicKey, faucetPrivateKey := getFaucetAccount()

	// Get faucet account info
	faucetAccount, err := service.AccountService.GetAccountByAddress(ctx, faucetPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get faucet account: %w", err)
	}

	// Send tokens from faucet to recipient
	faucetSeed := faucetPrivateKey.Seed()
	txHash, err := service.TxService.SendTokenWithoutDatabase(
		ctx,
		faucetAccount.Nonce+1,
		faucetPublicKey,
		toAddress,
		faucetSeed,
		amount,
		"Seed amount from faucet",
		mmnClient.TxTypeTransfer,
	)
	if err != nil {
		return "", fmt.Errorf("failed to send tokens from faucet: %w", err)
	}

	t.Logf("Seeding transaction sent: %s", txHash[:16])

	// Wait for transaction to be processed
	time.Sleep(5 * time.Second)

	// Verify the account now has enough balance
	updatedAccount, err := service.AccountService.GetAccountByAddress(ctx, toAddress)
	if err != nil {
		return "", fmt.Errorf("failed to get updated account: %w", err)
	}

	if updatedAccount.Balance.Cmp(amount) < 0 {
		return "", fmt.Errorf("account balance (%d) is less than seed amount (%d)", updatedAccount.Balance, amount)
	}

	t.Logf("Account seeded successfully! Balance: %d tokens", updatedAccount.Balance)

	return txHash, nil
}

func TestSendToken_Integration_Faucet(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	faucetPublicKey, faucetPrivateKey := getFaucetAccount()
	fmt.Println("faucetPublicKey", faucetPublicKey)
	// Convert previously hex-formatted recipient to base58
	hexRecipient := "9bd8e13668b1e5df346b666c5154541d3476591af7b13939ecfa32009f4bba7c"
	recBytes, _ := hex.DecodeString(hexRecipient)
	toAddress := base58.Encode(recBytes)

	// Check balance of toAddress
	account, err := service.AccountService.GetAccountByAddress(ctx, faucetPublicKey)
	if err != nil {
		t.Fatalf("Failed to get account balance: %v", err)
	}

	t.Logf("Account %s balance: %d tokens, nonce: %d", faucetPublicKey, account.Balance, account.Nonce)

	// Extract the seed from the private key (first 32 bytes)
	faucetSeed := faucetPrivateKey.Seed()
	txHash, err := service.TxService.SendTokenWithoutDatabase(ctx, account.Nonce+1, faucetPublicKey, toAddress, faucetSeed, uint256.NewInt(1), "Integration test transfer", mmnClient.TxTypeTransfer)
	if err != nil {
		t.Fatalf("SendTokenWithoutDatabase failed: %v", err)
	}

	t.Logf("Transaction successful! Hash: %s", txHash)

	time.Sleep(5 * time.Second)
	toAccount, err := service.AccountService.GetAccountByAddress(ctx, toAddress)
	if err != nil {
		t.Fatalf("Failed to get account balance: %v", err)
	}

	t.Logf("Account %s balance: %d tokens, nonce: %d", toAddress, toAccount.Balance, toAccount.Nonce)
}

// TestSendToken_Integration_RealMainnet tests the full flow with real mainnet
func TestSendToken_Integration_RealMainnet(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Test data
	fromUID := uint64(1)
	toUID := uint64(2)
	amount := utils.ToBigNumber(100) // Send minimal amount for testing
	textData := "Integration test transfer"

	// get amount from faucet account
	fromAccount, err := service.AccountService.GetAccount(ctx, fromUID)
	if err != nil {
		t.Fatalf("Failed to get account address %s: %v", fromAccount.Address, err)
	}
	fmt.Printf("fromAddr: %s\n", fromAccount.Address)
	seedAmount := utils.ToBigNumber(10000)
	_, err = seedAccountFromFaucet(t, ctx, service, fromAccount.Address, seedAmount)
	if err != nil {
		t.Fatalf("Failed to seed from account %s: %v", fromAccount.Address, err)
	}

	toAccount, err := service.AccountService.GetAccount(ctx, toUID)
	if err != nil {
		t.Fatalf("Failed to get account address %s: %v", toAccount.Address, err)
	}
	fmt.Printf("toAddr: %s\n", toAccount.Address)
	_, err = seedAccountFromFaucet(t, ctx, service, toAccount.Address, seedAmount)
	if err != nil {
		t.Fatalf("Failed to seed to account %s: %v", toAccount.Address, err)
	}

	// Act: Send token
	txHash, err := service.TxService.SendToken(ctx, fromAccount.Nonce+1, fromUID, toUID, amount, textData)

	// Assert
	if err != nil {
		t.Fatalf("SendToken failed: %v", err)
	}

	if txHash == "" {
		t.Fatal("Expected non-empty transaction hash")
	}

	t.Logf("Transaction successful! Hash: %s", txHash)
}

// TestSendToken_Integration_ExistingUsers tests sending between existing users
func TestSendToken_Integration_ExistingUsers(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	time.Sleep(3 * time.Second)
	ctx := context.Background()

	// Test data
	fromUID := uint64(1)
	toUID := uint64(2)
	amount := uint256.NewInt(5)
	textData := "Transfer between existing users"

	t.Logf("Sending tokens between existing users: %d -> %d", fromUID, toUID)

	// Act
	fromAccount, err := service.AccountService.GetAccount(ctx, fromUID)
	if err != nil {
		t.Fatalf("Failed to get account address %s: %v", fromAccount.Address, err)
	}
	txHash, err := service.TxService.SendToken(ctx, fromAccount.Nonce+1, fromUID, toUID, amount, textData)

	// Assert
	if err != nil {
		t.Fatalf("SendToken failed: %v", err)
	}

	if txHash == "" {
		t.Fatal("Expected non-empty transaction hash")
	}

	t.Logf("Transaction successful! Hash: %s", txHash)
}

func TestGiveCoffee_Integration_ExistingUsers(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	time.Sleep(3 * time.Second)
	ctx := context.Background()

	// Test data
	fromUID := uint64(1)
	toUID := uint64(2)

	t.Logf("Give coffee between existing users: %d -> %d", fromUID, toUID)

	// Act
	fromAccount, err := service.AccountService.GetAccount(ctx, fromUID)
	if err != nil {
		t.Fatalf("Failed to get account address %s: %v", fromAccount.Address, err)
	}

	txHash, err := service.TxService.GiveCoffee(ctx, fromAccount.Nonce+1, fromUID, toUID)
	if err != nil {
		t.Fatalf("GiveCoffee failed: %v", err)
	}

	// Assert
	if err != nil {
		t.Fatalf("SendToken failed: %v", err)
	}

	if txHash == "" {
		t.Fatal("Expected non-empty transaction hash")
	}

	t.Logf("Transaction successful! Hash: %s", txHash)
}

func TestUnlockItem_Integration_ExistingUsers(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	time.Sleep(3 * time.Second)
	ctx := context.Background()

	// Test data
	fromUID := uint64(1)
	toUID := uint64(2)
	itemUID := uint64(time.Now().Second())
	itemType := uint(1)

	t.Logf("Sending tokens to unlock item: %d -> %d", fromUID, toUID)

	// Act
	txHash, err := service.TxService.UnlockItem(ctx, 0, fromUID, toUID, itemUID, itemType)

	// Assert
	if err != nil {
		t.Fatalf("SendToken failed: %v", err)
	}

	if txHash == "" {
		t.Fatal("Expected non-empty transaction hash")
	}

	t.Logf("Transaction successful! Hash: %s", txHash)
}

// TestSendToken_Integration_MultipleTransactions tests sending multiple transactions
func TestSendToken_Integration_MultipleTransactions(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	time.Sleep(3 * time.Second)
	ctx := context.Background()

	fromUID := uint64(1)
	toUID := uint64(2)

	// Send multiple transactions to test nonce increment
	transactions := []struct {
		amount   *uint256.Int
		textData string
	}{
		{uint256.NewInt(1), "First transaction"},
		{uint256.NewInt(2), "Second transaction"},
		{uint256.NewInt(3), "Third transaction"},
	}

	var txHashes []string
	fromAccount, err := service.AccountService.GetAccount(ctx, fromUID)
	if err != nil {
		t.Fatalf("Failed to get account address %s: %v", fromAccount.Address, err)
	}

	for i, tx := range transactions {
		t.Logf("Sending transaction %d: amount=%d", i+1, tx.amount)

		txHash, err := service.TxService.SendToken(ctx, fromAccount.Nonce+1+uint64(i), fromUID, toUID, tx.amount, tx.textData)
		if err != nil {
			t.Fatalf("Transaction %d failed: %v", i+1, err)
		}

		if txHash == "" {
			t.Fatalf("Transaction %d returned empty hash", i+1)
		}

		txHashes = append(txHashes, txHash)
		t.Logf("Transaction %d successful: %s", i+1, txHash)

		// Small delay between transactions
		time.Sleep(1 * time.Second)
	}

	t.Logf("All %d transactions completed successfully", len(transactions))
	for i, hash := range txHashes {
		t.Logf("Transaction %d hash: %s", i+1, hash)
	}
}

// TestSendToken_Integration_ErrorCases tests error scenarios with real mainnet
func TestSendToken_Integration_ErrorCases(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("InvalidAmount", func(t *testing.T) {
		_, err := service.TxService.SendToken(ctx, 0, uint64(1), uint64(2), uint256.NewInt(0), "invalid amount")
		if err == nil {
			t.Fatal("Expected error for zero amount")
		}
		t.Logf("Correctly rejected zero amount: %v", err)
	})
}

func Test_GetListFaucetTransactions(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	faucetPublicKey, _ := getFaucetAccount()
	txs, err := service.TxService.ListFaucetTransactions(ctx, faucetPublicKey, 10, 1, 0)
	if err != nil {
		t.Fatalf("ListFaucetTransactions failed: %v", err)
	}

	t.Logf("ListTransactions: %v", txs)
}

// TestGetTxByHash_Integration tests the GetTxByHash functionality
func TestGetTxByHash_Integration(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// First, create a transaction to get a valid hash
	faucetPublicKey, faucetPrivateKey := getFaucetAccount()
	toUID := uint64(1)
	toAddress, err := service.TxService.GetAccountAddress(toUID)
	if err != nil {
		t.Fatalf("Failed to get account address %s: %v", toAddress, err)
	}

	// Send a transaction to get a valid hash
	faucetSeed := faucetPrivateKey.Seed()
	faucetAccount, err := service.AccountService.GetAccountByAddress(ctx, faucetPublicKey)
	if err != nil {
		t.Fatalf("GetAccountByAddress failed: %v", err)
	}
	fmt.Printf("faucetAccount.Nonce: %d\n", faucetAccount.Nonce)
	txHash, err := service.TxService.SendTokenWithoutDatabase(ctx, faucetAccount.Nonce+1, faucetPublicKey, toAddress, faucetSeed, uint256.NewInt(1), "GetTxByHash test transfer", mmnClient.TxTypeTransfer)
	if err != nil {
		t.Fatalf("Failed to create test transaction: %v", err)
	}

	t.Logf("Created test transaction with hash: %s", txHash)

	// Wait a bit for the transaction to be processed
	time.Sleep(3 * time.Second)

	// Test: Get transaction by hash
	txInfo, err := service.TxService.GetTxByHash(ctx, txHash)
	if err != nil {
		t.Fatalf("GetTxByHash failed: %v", err)
	}

	// Assert: Verify the transaction details
	if txInfo.Sender != faucetPublicKey {
		t.Errorf("Expected sender %s, got %s", faucetPublicKey, txInfo.Sender)
	}
	if txInfo.Recipient != toAddress {
		t.Errorf("Expected recipient %s, got %s", toAddress, txInfo.Recipient)
	}
	if txInfo.Amount.Cmp(uint256.NewInt(1)) != 0 {
		t.Errorf("Expected amount 1, got %d", txInfo.Amount)
	}
	if txInfo.TextData != "GetTxByHash test transfer" {
		t.Errorf("Expected text data 'GetTxByHash test transfer', got '%s'", txInfo.TextData)
	}

	t.Logf("Successfully retrieved transaction: sender=%s, recipient=%s, amount=%d, text=%s",
		txInfo.Sender, txInfo.Recipient, txInfo.Amount, txInfo.TextData)
}

// TestGetTxByHash_InvalidHash tests GetTxByHash with invalid hash
func TestGetTxByHash_InvalidHash(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Test with invalid hash
	invalidHash := "invalid_hash_format"
	_, err := service.TxService.GetTxByHash(ctx, invalidHash)
	if err == nil {
		t.Fatal("Expected error for invalid hash format")
	}

	t.Logf("Correctly rejected invalid hash: %v", err)
}

// TestGetBalanceAndTransactions_Integration_CompleteFlow tests the complete flow
func TestGetBalanceAndTransactions_Integration_CompleteFlow(t *testing.T) {
	svc, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Setup test data - transfer from one account to another
	toUID := uint64(1)
	toAddr, err := svc.TxService.GetAccountAddress(toUID)
	if err != nil {
		t.Fatalf("Failed to get account address %d: %v", toUID, err)
	}
	seedAmount := uint256.NewInt(25)
	transferAmount := uint256.NewInt(25)
	fromUID := uint64(2)
	fromAddr, err := svc.TxService.GetAccountAddress(fromUID)
	if err != nil {
		t.Fatalf("Failed to get account address %d: %v", fromUID, err)
	}

	// Seed the "from account" first
	t.Logf("Seeding amount %d for %s", seedAmount, fromAddr[:16])
	txHash, err := seedAccountFromFaucet(t, ctx, svc, fromAddr, seedAmount)
	if err != nil {
		t.Fatalf("Failed to seed from account: %v", err)
	}
	t.Logf("From account seeded successfully: %s", txHash[:16])

	// Get initial balances after seeding
	initialFromAccount, err := svc.AccountService.GetAccountByAddress(ctx, fromAddr)
	if err != nil {
		t.Fatalf("Failed to get initial from account: %v", err)
	}
	initialToAccount, err := svc.AccountService.GetAccountByAddress(ctx, toAddr)
	if err != nil {
		t.Fatalf("Failed to get initial to account: %v", err)
	}
	t.Logf("Initial state - From %s: balance=%d, nonce=%d", fromAddr[:16], initialFromAccount.Balance, initialFromAccount.Nonce)
	t.Logf("Initial state - To %s: balance=%d, nonce=%d", toAddr[:16], initialToAccount.Balance, initialToAccount.Nonce)

	// Perform the transfer from account 1 to account 2
	t.Logf("Performing transfer: %d tokens from %s to %s", transferAmount, fromAddr[:16], toAddr[:16])
	transferTxHash, err := svc.TxService.SendToken(ctx, initialFromAccount.Nonce+1, fromUID, toUID, transferAmount, "Account to account transfer")
	if err != nil {
		t.Fatalf("SendToken failed: %v", err)
	}
	t.Logf("Transfer transaction sent: %s", transferTxHash)

	// Wait for transaction to be processed
	time.Sleep(5 * time.Second)

	// Run child tests
	t.Run("TestGetBalance_UpdatedCorrectly", func(t *testing.T) {
		testGetBalanceUpdatedCorrectly(t, ctx, svc.TxService, fromAddr, toAddr, initialFromAccount, initialToAccount, transferAmount)
	})

	t.Run("TestListTransactions_ByAddress", func(t *testing.T) {
		testListTransactionsByAddress(t, ctx, svc.TxService, fromAddr, toAddr, transferAmount, transferTxHash)
	})
}

// testGetBalanceUpdatedCorrectly verifies that balances are updated correctly after the transfer
func testGetBalanceUpdatedCorrectly(t *testing.T, ctx context.Context, svc *service.TxService, fromAddr, toAddr string, initialFromAccount, initialToAccount mmnClient.Account, transferAmount *uint256.Int) {
	// Get updated balances after transfer
	updatedFromAccount, err := svc.GetAccountByAddress(ctx, fromAddr)
	if err != nil {
		t.Fatalf("Failed to get updated from account: %v", err)
	}

	updatedToAccount, err := svc.GetAccountByAddress(ctx, toAddr)
	if err != nil {
		t.Fatalf("Failed to get updated to account: %v", err)
	}

	t.Logf("Updated state - From account (%s): balance=%d, nonce=%d", fromAddr[:16], updatedFromAccount.Balance, updatedFromAccount.Nonce)
	t.Logf("Updated state - To account (%s): balance=%d, nonce=%d", toAddr[:16], updatedToAccount.Balance, updatedToAccount.Nonce)

	// Verify balances are updated correctly
	expectedFromBalance := new(uint256.Int).Sub(initialFromAccount.Balance, transferAmount)
	if updatedFromAccount.Balance.Cmp(expectedFromBalance) != 0 {
		t.Errorf("From account balance incorrect: expected %d, got %d",
			expectedFromBalance, updatedFromAccount.Balance)
	}

	expectedToBalance := new(uint256.Int).Add(initialToAccount.Balance, transferAmount)
	if updatedToAccount.Balance.Cmp(expectedToBalance) != 0 {
		t.Errorf("To account balance incorrect: expected %d, got %d",
			expectedToBalance, updatedToAccount.Balance)
	}
}

// testListTransactionsByAddress verifies transaction listing by address with filters
func testListTransactionsByAddress(t *testing.T, ctx context.Context, svc *service.TxService, fromAddr, toAddr string, transferAmount *uint256.Int, transferTxHash string) {
	// Test listing transactions for from address
	t.Run("FromAddress_AllTransactions", func(t *testing.T) {
		transactions, err := svc.ListTransactionsByAddress(ctx, fromAddr, 10, 1, 0) // filter=0 (all)
		if err != nil {
			t.Fatalf("ListTransactionsByAddress failed: %v", err)
		}

		t.Logf("All transactions for %s: count=%d", fromAddr[:16], transactions.Total)

		// Verify we have at least one transaction (the transfer)
		if transactions.Total < 1 {
			t.Errorf("Expected at least 1 transaction for from address %s, got %d", fromAddr[:16], transactions.Total)
		}
	})

	// Test listing sent transactions only (filter=1)
	t.Run("FromAddress_FilterSentTransactions", func(t *testing.T) {
		transactions, err := svc.ListTransactionsByAddress(ctx, fromAddr, 10, 1, 1) // filter=1 (sent)
		if err != nil {
			t.Fatalf("ListTransactionsByAddress (sent) failed: %v", err)
		}

		t.Logf("Transaction sent from %s: count=%d", fromAddr[:16], transactions.Total)

		if transactions.Total < 1 {
			t.Errorf("Expected at least 1 transaction for from address %s, got %d", fromAddr[:16], transactions.Total)
		}

		// Verify all transactions in the list are sent transactions (fromAddr is sender)
		for _, tx := range transactions.Txs {
			if tx.Sender != fromAddr {
				t.Errorf("Transaction of type sent for %s: Sender should be %s, got %s", fromAddr[:16], fromAddr, tx.Sender)
				break
			}
		}
	})
}

// TestHealthCheck_Integration tests the health check functionality with real mainnet nodes
func TestHealthCheck_Integration(t *testing.T) {
	// Get config from environment or use defaults
	endpoint := getEnvOrDefault("MMN_ENDPOINT", defaultMainnetEndpoints)

	// Parse endpoints (assuming comma-separated)
	endpoints := strings.Split(endpoint, ",")

	if len(endpoints) == 0 {
		t.Fatalf("No endpoints configured, health check test failed")
	}

	t.Run("BasicHealthCheck", func(t *testing.T) {
		for i, ep := range endpoints {
			t.Logf("Testing health check for endpoint %d: %s", i+1, ep)

			// Connect to the node
			conn, err := grpc.NewClient(ep, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Logf("Failed to connect to %s: %v", ep, err)
				continue
			}
			defer conn.Close()

			// Create health service client
			healthClient := mmnpb.NewHealthServiceClient(conn)

			// Test basic health check
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			resp, err := healthClient.Check(ctx, &mmnpb.Empty{})
			if err != nil {
				t.Fatalf("Health check failed for %s: %v", ep, err)
			}

			if resp.ErrorMessage != "" {
				t.Fatalf("Error Message: %s", resp.ErrorMessage)
			}

			// Log health check response
			t.Logf("Health check response from %s:", ep)
			t.Logf("  Status: %v", resp.Status)
			t.Logf("  Node ID: %s", resp.NodeId)
			t.Logf("  Current Slot: %d", resp.CurrentSlot)
			t.Logf("  Block Height: %d", resp.BlockHeight)
			t.Logf("  Mempool Size: %d", resp.MempoolSize)
			t.Logf("  Is Leader: %v", resp.IsLeader)
			t.Logf("  Is Follower: %v", resp.IsFollower)
			t.Logf("  Version: %s", resp.Version)
			t.Logf("  Uptime: %d seconds", resp.Uptime)

			// Basic validation
			if resp.NodeId == "" {
				t.Fatalf("Expected non-empty node ID from %s", ep)
			}

			if resp.Status == mmnpb.HealthCheckResponse_UNKNOWN {
				t.Fatalf("Warning: Node %s returned UNKNOWN status", ep)
			}

			if resp.Status == mmnpb.HealthCheckResponse_NOT_SERVING {
				t.Fatalf("Warning: Node %s is NOT_SERVING", ep)
			}
		}
	})
}
