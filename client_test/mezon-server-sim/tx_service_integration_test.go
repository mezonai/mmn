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

	"github.com/mezonai/mmn/client_test/mezon-server-sim/mezoncfg"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/adapter/blockchain"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/adapter/keystore"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/domain"
	pb "github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/proto"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/service"

	_ "github.com/lib/pq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Test configuration - set these via environment variables
const (
	defaultMainnetEndpoints = "localhost:9001,localhost:9002,localhost:9003" // Your local mainnet gRPC endpoint
	defaultDbURL            = "postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable"
	defaultMasterKey        = "bWV6b25fdGVzdF9tYXN0ZXJfa2V5XzEyMzQ1Njc4OTA=" // base64 of "mezon_test_master_key_1234567890"
)

func setupIntegrationTest(t *testing.T) (*service.TxService, func()) {
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
	config := mezoncfg.MmnConfig{
		Endpoints: endpoint,
		Timeout:   30000,
		ChainID:   "1",
		MasterKey: masterKey,
	}

	mainnetClient, err := blockchain.NewGRPCClient(config)
	if err != nil {
		t.Fatalf("Failed to create mainnet client: %v", err)
	}

	// Create TxService with real implementations
	service := service.NewTxService(mainnetClient, walletManager, db)

	// Cleanup function
	cleanup := func() {
		db.Close()
	}

	return service, cleanup
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
	faucetPublicKeyHex := hex.EncodeToString(faucetPublicKey[:])
	fmt.Println("faucetPublicKeyHex", faucetPublicKeyHex)
	return faucetPublicKeyHex, faucetPrivateKey
}

// seedAccountFromFaucet sends initial tokens from faucet to a given address
func seedAccountFromFaucet(t *testing.T, ctx context.Context, service *service.TxService, toAddress string, amount uint64) (string, error) {
	faucetPublicKey, faucetPrivateKey := getFaucetAccount()

	// Get faucet account info
	faucetAccount, err := service.GetAccountByAddress(ctx, faucetPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to get faucet account: %w", err)
	}

	// Send tokens from faucet to recipient
	faucetSeed := faucetPrivateKey.Seed()
	txHash, err := service.SendTokenWithoutDatabase(
		ctx,
		faucetAccount.Nonce,
		faucetPublicKey,
		toAddress,
		faucetSeed,
		amount,
		"Seed amount from faucet",
		domain.TxTypeFaucet,
	)
	if err != nil {
		return "", fmt.Errorf("failed to send tokens from faucet: %w", err)
	}

	t.Logf("Seeding transaction sent: %s", txHash[:16])

	// Wait for transaction to be processed
	time.Sleep(5 * time.Second)

	// Verify the account now has enough balance
	updatedAccount, err := service.GetAccountByAddress(ctx, toAddress)
	if err != nil {
		return "", fmt.Errorf("failed to get updated account: %w", err)
	}

	if updatedAccount.Balance < amount {
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
	fmt.Println("faucetPublicKey13", faucetPublicKey)
	toAddress := "9bd8e13668b1e5df346b666c5154541d3476591af7b13939ecfa32009f4bba7c"

	// Check balance of toAddress
	account, err := service.GetAccountByAddress(ctx, faucetPublicKey)
	if err != nil {
		t.Fatalf("Failed to get account balance: %v", err)
	}

	t.Logf("Account %s balance: %d tokens, nonce: %d", faucetPublicKey, account.Balance, account.Nonce)

	// Extract the seed from the private key (first 32 bytes)
	faucetSeed := faucetPrivateKey.Seed()
	txHash, err := service.SendTokenWithoutDatabase(ctx, 0, faucetPublicKey, toAddress, faucetSeed, 1, "Integration test transfer", domain.TxTypeFaucet)
	if err != nil {
		t.Fatalf("SendTokenWithoutDatabase failed: %v", err)
	}

	t.Logf("Transaction successful! Hash: %s", txHash)

	time.Sleep(5 * time.Second)
	toAccount, err := service.GetAccountByAddress(ctx, toAddress)
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
	amount := uint64(100) // Send minimal amount for testing
	textData := "Integration test transfer"

	t.Logf("Starting integration test: sending %d tokens from %d to %d", amount, fromUID, toUID)

	// Act: Send token
	txHash, err := service.SendToken(ctx, 0, fromUID, toUID, amount, textData)

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

	ctx := context.Background()

	// Test data
	fromUID := uint64(1)
	toUID := uint64(2)
	amount := uint64(5)
	textData := "Transfer between existing users"

	t.Logf("Sending tokens between existing users: %d -> %d", fromUID, toUID)

	// Act
	txHash, err := service.SendToken(ctx, 0, fromUID, toUID, amount, textData)

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

	ctx := context.Background()

	// Test data
	fromUID := uint64(1)
	toUID := uint64(2)

	t.Logf("Give coffee between existing users: %d -> %d", fromUID, toUID)

	// Act
	txHash, err := service.GiveCoffee(ctx, 0, fromUID, toUID)

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

	ctx := context.Background()

	// Test data
	fromUID := uint64(1)
	toUID := uint64(2)
	itemUID := uint64(5)
	itemType := "testing"

	t.Logf("Sending tokens to unlock item: %d -> %d", fromUID, toUID)

	// Act
	txHash, err := service.UnlockItem(ctx, 0, fromUID, toUID, itemUID, itemType)

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

	ctx := context.Background()

	fromUID := uint64(1)
	toUID := uint64(2)

	// Send multiple transactions to test nonce increment
	transactions := []struct {
		amount   uint64
		textData string
	}{
		{1, "First transaction"},
		{2, "Second transaction"},
		{3, "Third transaction"},
	}

	var txHashes []string

	for i, tx := range transactions {
		t.Logf("Sending transaction %d: amount=%d", i+1, tx.amount)

		txHash, err := service.SendToken(ctx, 0, fromUID, toUID, tx.amount, tx.textData)
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
		_, err := service.SendToken(ctx, 0, uint64(1), uint64(2), 0, "invalid amount")
		if err == nil {
			t.Fatal("Expected error for zero amount")
		}
		t.Logf("Correctly rejected zero amount: %v", err)
	})

	t.Run("LargeAmount", func(t *testing.T) {
		// Test with very large amount (should fail if insufficient balance)
		_, err := service.SendToken(ctx, 0, uint64(1), uint64(2), ^uint64(0), "large amount")
		if err == nil {
			t.Log("Large amount transaction succeeded (user might have sufficient balance)")
		} else {
			t.Logf("Large amount transaction failed as expected: %v", err)
		}
	})
}

// Benchmark test for performance measurement
func BenchmarkSendToken_Integration(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	service, cleanup := setupIntegrationTest(&testing.T{})
	defer cleanup()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			fromUID := uint64(i)
			toUID := uint64(i + 1)

			_, err := service.SendToken(ctx, 0, fromUID, toUID, 1, "benchmark test")
			if err != nil {
				b.Errorf("SendToken failed: %v", err)
			}
			i++
		}
	})
}

func Test_GetListFaucetTransactions(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	txs, err := service.ListFaucetTransactions(ctx, 10, 1, 0)
	if err != nil {
		t.Fatalf("ListTransactions failed: %v", err)
	}

	t.Logf("ListTransactions: %v", txs)
}

// TestGetBalanceAndTransactions_Integration_CompleteFlow tests the complete flow
func TestGetBalanceAndTransactions_Integration_CompleteFlow(t *testing.T) {
	svc, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Setup test data - transfer from one account to another
	toAddr := "8ac7e13668b1e5df346b666c5154541d3476591af7b13939ecfa32009f4bba7d"
	seedAmount := uint64(25)
	transferAmount := uint64(25)
	fromPriv := []byte{216, 225, 123, 4, 170, 149, 32, 216, 126, 223, 75, 46, 184, 101, 133, 247, 98, 166, 96, 57, 12, 104, 188, 249, 247, 23, 108, 201, 37, 25, 40, 231}
	fromPrivateKey := ed25519.NewKeyFromSeed(fromPriv)
	fromPublicKey := fromPrivateKey.Public().(ed25519.PublicKey)
	fromAddr := hex.EncodeToString(fromPublicKey[:])

	// Seed the "from account" first
	t.Logf("Seeding amount %d for %s", seedAmount, fromAddr[:16])
	txHash, err := seedAccountFromFaucet(t, ctx, svc, fromAddr, seedAmount)
	if err != nil {
		t.Fatalf("Failed to seed from account: %v", err)
	}
	t.Logf("From account seeded successfully: %s", txHash[:16])

	// Get initial balances after seeding
	initialFromAccount, err := svc.GetAccountByAddress(ctx, fromAddr)
	if err != nil {
		t.Fatalf("Failed to get initial from account: %v", err)
	}
	initialToAccount, err := svc.GetAccountByAddress(ctx, toAddr)
	if err != nil {
		t.Fatalf("Failed to get initial to account: %v", err)
	}
	t.Logf("Initial state - From %s: balance=%d, nonce=%d", fromAddr[:16], initialFromAccount.Balance, initialFromAccount.Nonce)
	t.Logf("Initial state - To %s: balance=%d, nonce=%d", toAddr[:16], initialToAccount.Balance, initialToAccount.Nonce)

	// Perform the transfer from account 1 to account 2
	t.Logf("Performing transfer: %d tokens from %s to %s", transferAmount, fromAddr[:16], toAddr[:16])
	transferTxHash, err := svc.SendTokenWithoutDatabase(ctx, initialFromAccount.Nonce+1, fromAddr, toAddr, fromPriv, transferAmount, "Account to account transfer", domain.TxTypeTransfer)
	if err != nil {
		t.Fatalf("SendTokenWithoutDatabase failed: %v", err)
	}
	t.Logf("Transfer transaction sent: %s", transferTxHash)

	// Wait for transaction to be processed
	time.Sleep(5 * time.Second)

	// Run child tests
	t.Run("TestGetBalance_UpdatedCorrectly", func(t *testing.T) {
		testGetBalanceUpdatedCorrectly(t, ctx, svc, fromAddr, toAddr, initialFromAccount, initialToAccount, transferAmount)
	})

	t.Run("TestListTransactions_ByAddress", func(t *testing.T) {
		testListTransactionsByAddress(t, ctx, svc, fromAddr, toAddr, transferAmount, transferTxHash)
	})
}

// testGetBalanceUpdatedCorrectly verifies that balances are updated correctly after the transfer
func testGetBalanceUpdatedCorrectly(t *testing.T, ctx context.Context, svc *service.TxService, fromAddr, toAddr string, initialFromAccount, initialToAccount domain.Account, transferAmount uint64) {
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
	expectedFromBalance := initialFromAccount.Balance - transferAmount
	if updatedFromAccount.Balance != expectedFromBalance {
		t.Errorf("From account balance incorrect: expected %d, got %d",
			expectedFromBalance, updatedFromAccount.Balance)
	}

	expectedToBalance := initialToAccount.Balance + transferAmount
	if updatedToAccount.Balance != expectedToBalance {
		t.Errorf("To account balance incorrect: expected %d, got %d",
			expectedToBalance, updatedToAccount.Balance)
	}
}

// testListTransactionsByAddress verifies transaction listing by address with filters
func testListTransactionsByAddress(t *testing.T, ctx context.Context, svc *service.TxService, fromAddr, toAddr string, transferAmount uint64, transferTxHash string) {
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
			healthClient := pb.NewHealthServiceClient(conn)

			// Test basic health check
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			resp, err := healthClient.Check(ctx, &pb.Empty{})
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

			if resp.Status == pb.HealthCheckResponse_UNKNOWN {
				t.Fatalf("Warning: Node %s returned UNKNOWN status", ep)
			}

			if resp.Status == pb.HealthCheckResponse_NOT_SERVING {
				t.Fatalf("Warning: Node %s is NOT_SERVING", ep)
			}
		}
	})

	// t.Run("HealthCheckStreaming", func(t *testing.T) {
	// 	for i, ep := range endpoints {
	// 		t.Logf("Testing streaming health check for endpoint %d: %s", i+1, ep)

	// 		conn, err := grpc.NewClient(ep, grpc.WithTransportCredentials(insecure.NewCredentials()))
	// 		if err != nil {
	// 			t.Logf("Failed to connect to %s: %v", ep, err)
	// 			continue
	// 		}
	// 		defer conn.Close()

	// 		healthClient := pb.NewHealthServiceClient(conn)

	// 		req := &pb.HealthCheckRequest{
	// 			NodeId:    fmt.Sprintf("test-client-stream-%d", i+1),
	// 			Timestamp: uint64(time.Now().Unix()),
	// 		}

	// 		// Test streaming health check
	// 		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	// 		defer cancel()

	// 		stream, err := healthClient.Watch(ctx, req)
	// 		if err != nil {
	// 			t.Logf("Streaming health check failed for %s: %v", ep, err)
	// 			continue
	// 		}

	// 		// Receive a few health updates
	// 		updateCount := 0
	// 		maxUpdates := 3

	// 		for updateCount < maxUpdates {
	// 			resp, err := stream.Recv()
	// 			if err != nil {
	// 				t.Logf("Stream receive failed for %s: %v", ep, err)
	// 				break
	// 			}

	// 			t.Logf("Health update %d from %s: Slot=%d, Status=%v",
	// 				updateCount+1, ep, resp.CurrentSlot, resp.Status)

	// 			updateCount++

	// 			// Small delay to avoid overwhelming the stream
	// 			time.Sleep(1 * time.Second)
	// 		}

	// 		t.Logf("Received %d health updates from %s", updateCount, ep)
	// 	}
	// })
}
