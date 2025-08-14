package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mmn/client_test/mezon-server-sim/mezoncfg"
	"mmn/client_test/mezon-server-sim/mmn/adapter/blockchain"
	"mmn/client_test/mezon-server-sim/mmn/adapter/keystore"
	"mmn/client_test/mezon-server-sim/mmn/service"

	_ "github.com/lib/pq"
)

const (
	defaultMainnetEndpoint = "localhost:9001"
	defaultDbURL           = "postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable"
	defaultMasterKey       = "bWV6b25fdGVzdF9tYXN0ZXJfa2V5XzEyMzQ1Njc4OTA="
)

// Example demonstrating how to use the SubscribeTransactionStatus function
func main() {
	// Get configuration from environment or use defaults
	endpoint := getEnvOrDefault("MMN_ENDPOINT", defaultMainnetEndpoint)
	dbURL := getEnvOrDefault("DATABASE_URL", defaultDbURL)
	masterKey := getEnvOrDefault("MASTER_KEY", defaultMasterKey)

	log.Printf("Starting transaction status subscriber...")
	log.Printf("MMN Endpoint: %s", endpoint)
	log.Printf("Database URL: %s", maskPassword(dbURL))

	// Setup database connection
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Test database connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := db.PingContext(ctx); err != nil {
		cancel()
		log.Fatalf("Failed to ping database: %v", err)
	}
	cancel()

	// Create database table for unlocked items if it doesn't exist
	if err := createUnlockedItemsTable(db); err != nil {
		log.Fatalf("Failed to unlocked items status table: %v", err)
	}

	// Setup MMN client configuration
	config := mezoncfg.MmnConfig{
		Endpoint: endpoint,
		Timeout:  5000, // 5 seconds
	}

	// Create blockchain client
	mainnetClient, err := blockchain.NewGRPCClient(config)
	if err != nil {
		log.Fatalf("Failed to create blockchain client: %v", err)
	}

	// Create wallet manager
	walletManager, err := keystore.NewPgEncryptedStore(db, masterKey)
	if err != nil {
		log.Fatalf("Failed to create wallet manager: %v", err)
	}

	// Create transaction service
	txService := service.NewTxService(mainnetClient, walletManager, db)

	// Setup graceful shutdown
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start the transaction status subscriber in a goroutine
	go func() {
		log.Printf("Starting transaction status subscription...")
		if err := txService.SubscribeTransactionStatus(ctx); err != nil {
			if ctx.Err() != context.Canceled {
				log.Printf("Transaction status subscription error: %v", err)
			}
		}
	}()

	// Wait for interrupt signal
	<-sigChan
	log.Printf("Received shutdown signal, gracefully shutting down...")

	// Cancel the context to stop the subscription
	cancel()

	// Give some time for graceful shutdown
	time.Sleep(2 * time.Second)
	log.Printf("Shutdown complete")
}

// Helper function to get environment variable or return default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Helper function to mask password in database URL for logging
func maskPassword(dbURL string) string {
	// Simple masking - in production you might want more sophisticated masking
	return "postgres://mezon:***@localhost:5432/mezon?sslmode=disable"
}

// createUnlockedItemsTable creates the transaction_status table if it doesn't exist
func createUnlockedItemsTable(db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS unlocked_items (
	 		id SERIAL PRIMARY KEY,
	 		item_id BIGINT NOT NULL,
	 		user_id BIGINT NOT NULL,
	 		tx_hash TEXT UNIQUE NOT NULL,
	 		item_type VARCHAR(50) NOT NULL,
	 		status INT NOT NULL,
	 		created_at TIMESTAMPTZ DEFAULT now(),
	 		updated_at TIMESTAMPTZ DEFAULT now()
		);
	`

	_, err := db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create unlocked_items table: %w", err)
	}

	log.Printf("Unlocked items table ready!")
	return nil
}
