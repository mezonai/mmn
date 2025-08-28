package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/holiman/uint256"
	mmnClient "github.com/mezonai/mmn/client"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/keystore"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/service"
)

const (
	defaultMainnetEndpoints = "localhost:9001" // Your local mainnet gRPC endpoint
	defaultDbURL            = "postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable"
	defaultMasterKey        = "bWV6b25fdGVzdF9tYXN0ZXJfa2V5XzEyMzQ1Njc4OTA=" // base64 of "mezon_test_master_key_1234567890"
)

const totalRequests = 2000
const concurrency = 2000 // Number of concurrent requests

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

func TestPerformance_SendToken(t *testing.T) {
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

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
	var wg sync.WaitGroup
	var successCount, failureCount int64
	var latencies []time.Duration
	var mu sync.Mutex
	var errorLogs []ErrorLog
	var errorMu sync.Mutex

	start := time.Now()

	// Run Set requests
	for i := 0; i < totalRequests; i++ {
		if i%concurrency == 0 && i > 0 {
			wg.Wait() // Wait for previous batch to complete
		}
		wg.Add(1)
		go sendToken(t, service, i, &wg, &successCount, &failureCount, &latencies, &mu, &errorLogs, &errorMu)
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
	throughput := float64(totalRequests) / totalTime.Seconds()

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

func sendToken(t *testing.T, service *service.TxService, key int, wg *sync.WaitGroup, success, failure *int64, latencies *[]time.Duration, mu *sync.Mutex, errorLogs *[]ErrorLog, errorMu *sync.Mutex) {
	defer wg.Done()

	// Test data
	// fromUID := uint64(1)
	// toUID := uint64(2)
	fromAddr := "0b341da31ed91c8aa159d1dfeff1761795c84f70d00bddff2fa58147e6e3b493"
	toAddr := "9bd8e13668b1e5df346b666c5154541d3476591af7b13939ecfa32009f4bba7c"
	fromPriv := []byte{216, 225, 123, 4, 170, 149, 32, 216, 126, 223, 75, 46, 184, 101, 133, 247, 98, 166, 96, 57, 12, 104, 188, 249, 247, 23, 108, 201, 37, 25, 40, 231}
	amount := uint256.NewInt(1) // Send minimal amount for testing
	textData := fmt.Sprintf("Integration test transfer %d", key)

	ctx := context.Background()
	start := time.Now()
	// _, err := service.SendToken(ctx, 0, fromUID, toUID, amount, textData)
	_, err := service.SendTokenWithoutDatabase(ctx, 0, fromAddr, toAddr, fromPriv, amount, textData, mmnClient.TxTypeTransfer)
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
