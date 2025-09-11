package performance_test

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/holiman/uint256"
	_ "github.com/lib/pq" // PostgreSQL driver
	mmnClient "github.com/mezonai/mmn/client"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/keystore"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/service"
	"github.com/mr-tron/base58"
)

func init() {
	// Set required environment variables before any package initialization
	os.Setenv("LOGFILE_MAX_SIZE_MB", "100")
	os.Setenv("LOGFILE_MAX_AGE_DAYS", "7")
}

const (
	defaultMainnetEndpoints = "localhost:9001" // Your local mainnet gRPC endpoint
	defaultDbURL            = "postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable"
	defaultMasterKey        = "bWV6b25fdGVzdF9tYXN0ZXJfa2V5XzEyMzQ1Njc4OTA=" // base64 of "mezon_test_master_key_1234567890"
	// Faucet seed from realistic_tps_test.go
	faucetSeedHex = "8e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee"
)

const totalRequests = 600
const concurrency = 10
const accountsCount = 10

type TestResult struct {
	TotalTime      string  `json:"total_time"`
	TotalRequests  int     `json:"total_requests"`
	Concurrency    int     `json:"concurrency"`
	SuccessCount   int64   `json:"success_count"`
	FailureCount   int64   `json:"failure_count"`
	AverageLatency string  `json:"average_latency"`
	Throughput     float64 `json:"throughput"`
}

type ErrorLog struct {
	Timestamp string `json:"timestamp"`
	RequestID int    `json:"request_id"`
	Error     string `json:"error"`
	TextData  string `json:"text_data"`
}

// TestSetup_AccountsAndSeed - Test to create accounts and seed tokens
func TestSetup_AccountsAndSeed(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	accounts, _ := createMultipleAccounts(t, service, accountsCount)

	t.Logf("Created %d accounts successfully", len(accounts))
	for i, addr := range accounts {
		t.Logf("Account %d: %s", i, addr)
	}
}

// TestPerformance_SendToken - Test performance of sending tokens (only test send, no account creation)
func TestPerformance_SendToken(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	// Only run performance test, no account creation (assume accounts already exist)
	runPerformanceTestSendToken(t, service)
}

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

	// Configure connection pool to prevent "too many clients" error
	db.SetMaxOpenConns(50)                 // Maximum number of open connections
	db.SetMaxIdleConns(10)                 // Maximum number of idle connections
	db.SetConnMaxLifetime(5 * time.Minute) // Maximum lifetime of a connection
	db.SetConnMaxIdleTime(2 * time.Minute) // Maximum idle time of a connection

	// Test database connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
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

func deriveAddressFromSeed(seed []byte) string {
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	return base58.Encode(pub)
}

// Get existing accounts (assume they were created previously)
func getExistingAccounts(t *testing.T, service *service.TxService, count int) ([]string, [][]byte) {
	accounts := make([]string, count)
	seeds := make([][]byte, count)

	// Use faucet account as first account
	faucetSeed, _ := hex.DecodeString(faucetSeedHex)
	accounts[0] = deriveAddressFromSeed(faucetSeed)
	seeds[0] = faucetSeed

	// Create other accounts with different seeds (assume they already exist)
	for i := 1; i < count; i++ {
		// Create different seed for each account
		seed := make([]byte, 32)
		for j := 0; j < 32; j++ {
			seed[j] = byte(i*100 + j) // Create different seeds
		}
		accounts[i] = deriveAddressFromSeed(seed)
		seeds[i] = seed
	}

	return accounts, seeds
}

// Create multiple accounts with different seeds and seed tokens from faucet
func createMultipleAccounts(t *testing.T, service *service.TxService, count int) ([]string, [][]byte) {
	accounts := make([]string, count)
	seeds := make([][]byte, count)
	ctx := context.Background()

	// Use faucet account as first account
	faucetSeed, _ := hex.DecodeString(faucetSeedHex)
	accounts[0] = deriveAddressFromSeed(faucetSeed)
	seeds[0] = faucetSeed

	// Create other accounts with different seeds and seed tokens from faucet
	for i := 1; i < count; i++ {
		// Create different seed for each account
		seed := make([]byte, 32)
		for j := 0; j < 32; j++ {
			seed[j] = byte(i*100 + j) // Create different seeds
		}
		accounts[i] = deriveAddressFromSeed(seed)
		seeds[i] = seed

		// Seed tokens from faucet for new account
		seedAmount := uint256.NewInt(1000) // Seed 1000 tokens
		_, err := seedAccountFromFaucet(t, ctx, service, accounts[i], seedAmount)
		if err != nil {
			t.Logf("Warning: Failed to seed account %d: %v", i, err)
		} else {
			t.Logf("Successfully seeded account %d with %d tokens", i, seedAmount)
		}
	}

	return accounts, seeds
}

// seedAccountFromFaucet sends initial tokens from faucet to a given address
func seedAccountFromFaucet(t *testing.T, ctx context.Context, service *service.TxService, toAddress string, amount *uint256.Int) (string, error) {
	faucetSeed, _ := hex.DecodeString(faucetSeedHex)
	faucetAddr := deriveAddressFromSeed(faucetSeed)

	// Get faucet account info
	faucetAccount, err := service.GetAccountByAddress(ctx, faucetAddr)
	if err != nil {
		return "", fmt.Errorf("failed to get faucet account: %w", err)
	}

	// Send tokens from faucet to recipient
	txHash, err := service.SendTokenWithoutDatabase(
		ctx,
		faucetAccount.Nonce+1,
		faucetAddr,
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

	// No need to wait for processing - just submit to mempool for performance test
	// time.Sleep(2 * time.Second)

	return txHash, nil
}

func createTestAccounts(t *testing.T, service *service.TxService) {
	ctx := context.Background()

	// Use faucet account as sender (should exist and have balance)
	faucetSeed, err := hex.DecodeString(faucetSeedHex)
	if err != nil {
		t.Fatalf("Failed to decode faucet seed: %v", err)
	}
	faucetAddr := deriveAddressFromSeed(faucetSeed)

	// Create recipient account (toAddr)
	toSeed := []byte{9, 189, 142, 19, 102, 139, 30, 93, 243, 70, 182, 102, 197, 21, 69, 65, 211, 71, 101, 154, 247, 185, 57, 236, 250, 50, 0, 159, 75, 186, 124, 255}
	toAddr := deriveAddressFromSeed(toSeed)

	t.Logf("Using faucet account as sender...")
	t.Logf("Faucet address: %s", faucetAddr)
	t.Logf("Recipient address: %s", toAddr)

	// Check if faucet account exists
	_, err = service.GetAccountByAddress(ctx, faucetAddr)
	if err != nil {
		t.Logf("Faucet account does not exist: %v", err)
	} else {
		t.Logf("Faucet account exists and ready to use")
	}

	// Check recipient account
	_, err = service.GetAccountByAddress(ctx, toAddr)
	if err != nil {
		t.Logf("Recipient account does not exist, will be created during transaction")
	} else {
		t.Logf("Recipient account exists")
	}
}

func runPerformanceTestSendToken(t *testing.T, service *service.TxService) {
	t.Logf("Starting performance test with totalRequests=%d, concurrency=%d", totalRequests, concurrency)
	result := runTest(t, service)

	// Write result to file
	if err := writeResultToFile(t, result, "performance_results.json"); err != nil {
		t.Errorf("Failed to write result to file: %v", err)
	}

	fmt.Println("------------------Result------------------------")
	fmt.Println("total_time:", result.TotalTime)
	fmt.Println("total_requests:", result.TotalRequests)
	fmt.Println("concurrency: ", result.Concurrency)
	fmt.Println("success_count:", result.SuccessCount)
	fmt.Println("failure_count:", result.FailureCount)
	fmt.Println("TPS:", result.Throughput)
}

func runTest(t *testing.T, service *service.TxService) TestResult {
	var successCount, failureCount int64
	var latencies []time.Duration
	var mu sync.Mutex
	var errorLogs []ErrorLog
	var errorMu sync.Mutex

	// Use existing accounts (assume they were created previously)
	accounts, seeds := getExistingAccounts(t, service, accountsCount)

	// Create nonce for each account
	accountNonces := make([]uint64, accountsCount)
	ctx := context.Background()

	for i, addr := range accounts {
		account, err := service.GetAccountByAddress(ctx, addr)
		if err != nil {
			t.Fatalf("Failed to get account %d: %v", i, err)
		}
		accountNonces[i] = account.Nonce + 1
	}

	// Start measuring REAL performance test time
	start := time.Now()

	// Run requests with concurrency using different accounts
	var wg sync.WaitGroup
	for i := 0; i < totalRequests; i++ {
		// Select account based on index (round-robin)
		accountIndex := i % accountsCount

		if i%concurrency == 0 && i > 0 {
			wg.Wait() // Wait for previous batch to complete
		}
		wg.Add(1)
		go sendTokenWithAccount(t, service, i, &wg, &successCount, &failureCount, &latencies, &mu, &errorLogs, &errorMu, accounts[accountIndex], seeds[accountIndex], &accountNonces[accountIndex])
	}
	wg.Wait()

	totalTime := time.Since(start)

	// Write error logs to file
	if len(errorLogs) > 0 {
		if err := writeErrorLogsToFile(t, errorLogs, "error_logs.json"); err != nil {
			t.Errorf("Failed to write error logs to file: %v", err)
		}
	}

	// Calculate metrics
	var totalLatency time.Duration
	for _, latency := range latencies {
		totalLatency += latency
	}
	averageLatency := time.Duration(0)
	if len(latencies) > 0 {
		averageLatency = totalLatency / time.Duration(len(latencies))
	}
	// TPS = successful transactions / real time
	throughput := float64(successCount) / totalTime.Seconds()

	return TestResult{
		TotalTime:      fmt.Sprintf("%f(s)", totalTime.Seconds()),
		TotalRequests:  totalRequests,
		Concurrency:    concurrency,
		SuccessCount:   successCount,
		FailureCount:   failureCount,
		AverageLatency: fmt.Sprintf("%f(s)", averageLatency.Seconds()),
		Throughput:     throughput,
	}
}

func sendTokenWithAccount(t *testing.T, service *service.TxService, key int, wg *sync.WaitGroup, success, failure *int64, latencies *[]time.Duration, mu *sync.Mutex, errorLogs *[]ErrorLog, errorMu *sync.Mutex, fromAddr string, fromSeed []byte, currentNonce *uint64) {
	defer wg.Done()

	// Create recipient account (toAddr) - use different account
	toSeed := []byte{9, 189, 142, 19, 102, 139, 30, 93, 243, 70, 182, 102, 197, 21, 69, 65, 211, 71, 101, 154, 247, 185, 57, 236, 250, 50, 0, 159, 75, 186, 124, 255}
	toAddr := deriveAddressFromSeed(toSeed)

	amount := uint256.NewInt(1) // Send minimal amount for testing
	textData := fmt.Sprintf("Integration test transfer %d", key)

	ctx := context.Background()

	// Use nonce that has been updated for this account
	myNonce := *currentNonce
	*currentNonce++ // Increment nonce for next time

	start := time.Now()
	_, err := service.SendTokenWithoutDatabase(ctx, myNonce, fromAddr, toAddr, fromSeed, amount, textData, mmnClient.TxTypeTransfer)
	latency := time.Since(start)

	mu.Lock()
	*latencies = append(*latencies, latency)
	mu.Unlock()

	if err != nil {
		// Log error to file
		errorLog := ErrorLog{
			Timestamp: time.Now().Format("2006-01-02 15:04:05.000"),
			RequestID: key,
			Error:     err.Error(),
			TextData:  textData,
		}

		errorMu.Lock()
		*errorLogs = append(*errorLogs, errorLog)
		errorMu.Unlock()

		t.Errorf("Send token failed for: %v, %v", err, textData)
		atomic.AddInt64(failure, 1)
		return
	}

	atomic.AddInt64(success, 1)
}

func writeResultToFile(t *testing.T, result TestResult, filename string) error {
	// Marshal result to JSON
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal result to JSON: %w", err)
	}

	// Write to file
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write result to file %s: %w", filename, err)
	}

	t.Logf("Test result written to %s", filename)
	return nil
}

func writeErrorLogsToFile(t *testing.T, errorLogs []ErrorLog, filename string) error {
	// Marshal error logs to JSON
	data, err := json.MarshalIndent(errorLogs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal error logs to JSON: %w", err)
	}

	// Write to file
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write error logs to file %s: %w", filename, err)
	}

	t.Logf("Error logs written to %s (total errors: %d)", filename, len(errorLogs))
	return nil
}
