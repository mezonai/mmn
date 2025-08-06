package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"mezon/v2/mezoncfg"
	"mezon/v2/mmn/adapter/blockchain"
	"mezon/v2/mmn/adapter/keystore"
	"mezon/v2/mmn/service"

	_ "github.com/lib/pq"
)

// Test configuration - set these via environment variables
const (
	defaultMainnetEndpoint = "localhost:9001" // Your local mainnet gRPC endpoint
	defaultDbURL           = "postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable"
	defaultMasterKey       = "bWV6b25fdGVzdF9tYXN0ZXJfa2V5XzEyMzQ1Njc4OTA=" // base64 của "mezon_test_master_key_1234567890"
)

func setupIntegrationTest(t *testing.T) (*service.TxService, func()) {
	t.Helper()

	// Get config from environment or use defaults
	endpoint := getEnvOrDefault("MMN_ENDPOINT", defaultMainnetEndpoint)
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
		Endpoint:  endpoint,
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
