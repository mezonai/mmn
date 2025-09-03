package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// ConnectDatabase establishes connection to PostgreSQL database with retry mechanism
func ConnectDatabase(databaseURL string) (*sql.DB, error) {
	const maxRetries = 5
	const retryDelay = time.Second * 3

	var db *sql.DB
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("⏳ Retrying database connection (attempt %d/%d) after error: %v", attempt+1, maxRetries, lastErr)
			time.Sleep(retryDelay)
		}

		var err error
		db, err = sql.Open("postgres", databaseURL)
		if err != nil {
			lastErr = fmt.Errorf("failed to open database connection: %v", err)
			continue
		}

		// Test the connection with timeout
		if err := db.Ping(); err != nil {
			db.Close()
			lastErr = fmt.Errorf("failed to ping database: %v", err)
			continue
		}

		// Connection successful
		log.Printf("✅ Database connection established successfully")
		return db, nil
	}

	return nil, fmt.Errorf("failed to connect to database after %d attempts: %v", maxRetries, lastErr)
}

// CreateUserKeysTable creates the mmn_user_keys table if it doesn't exist
func CreateUserKeysTable(db *sql.DB) error {
	// Drop existing table to ensure schema is up to date
	// dropTableSQL := `DROP TABLE IF EXISTS mmn_user_keys;`
	// _, err := db.Exec(dropTableSQL)
	// if err != nil {
	// 	return fmt.Errorf("failed to drop existing mmn_user_keys table: %v", err)
	// }

	createTableSQL := `
	CREATE TABLE IF NOT EXISTS mmn_user_keys (
		user_id      BIGINT PRIMARY KEY,
		address      VARCHAR(255) NOT NULL,
		enc_privkey  BYTEA NOT NULL,
		created_at   TIMESTAMPTZ DEFAULT now(),
		updated_at   TIMESTAMPTZ DEFAULT now()
	);
	`

	_, err := db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create mmn_user_keys table: %v", err)
	}

	log.Println("✅ mmn_user_keys table ready")
	return nil
}

// GetUsers retrieves all users from the users table excluding HRM user
func GetUsers(db *sql.DB) ([]map[string]interface{}, error) {
	rows, err := db.Query("SELECT id, username, wallet FROM users WHERE username != $1 ORDER BY id", HRM_USERNAME)
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %v", err)
	}
	defer rows.Close()

	var users []map[string]interface{}
	for rows.Next() {
		var id int
		var username string
		var wallet int64
		if err := rows.Scan(&id, &username, &wallet); err != nil {
			return nil, fmt.Errorf("failed to scan user row: %v", err)
		}
		users = append(users, map[string]interface{}{
			"id":      id,
			"name":    username,
			"balance": wallet,
		})
	}

	return users, nil
}

// CheckExistingWallet checks if a wallet already exists for the given user ID
func CheckExistingWallet(db *sql.DB, userID int) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM mmn_user_keys WHERE user_id = $1", userID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check existing wallet: %v", err)
	}
	return count > 0, nil
}

// CountExistingWallets counts the number of existing wallets in the database
func CountExistingWallets(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM mmn_user_keys").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count existing wallets: %v", err)
	}
	return count, nil
}

// CheckUserExists checks if a user with given username exists
func CheckUserExists(db *sql.DB, username string) (bool, int, int64, error) {
	var id int
	var wallet int64
	err := db.QueryRow("SELECT id, wallet FROM users WHERE username = $1", username).Scan(&id, &wallet)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, 0, 0, nil
		}
		return false, 0, 0, fmt.Errorf("failed to check user existence: %v", err)
	}
	return true, id, wallet, nil
}

// CreateUser creates a new user with given username and wallet balance
func CreateUser(db *sql.DB, username string, walletBalance int64) (int, error) {
	var userID int

	// First try with auto-increment ID (PostgreSQL SERIAL)
	err := db.QueryRow("INSERT INTO users (username, wallet) VALUES ($1, $2) RETURNING id",
		username, walletBalance).Scan(&userID)

	if err != nil {
		// Check if it's a NOT NULL constraint on id column
		if strings.Contains(err.Error(), `null value in column "id"`) {
			// Get the next available ID manually
			var maxID int
			err2 := db.QueryRow("SELECT COALESCE(MAX(id), 0) + 1 FROM users").Scan(&maxID)
			if err2 != nil {
				return 0, fmt.Errorf("failed to get next user ID: %v (original error: %v)", err2, err)
			}

			// Try inserting with explicit ID
			err = db.QueryRow("INSERT INTO users (id, username, wallet) VALUES ($1, $2, $3) RETURNING id",
				maxID, username, walletBalance).Scan(&userID)
			if err != nil {
				return 0, fmt.Errorf("failed to create user with explicit ID %d: %v", maxID, err)
			}

			log.Printf("✅ Created user '%s' with manually assigned ID %d", username, userID)
			return userID, nil
		}

		// Other errors
		return 0, fmt.Errorf("failed to create user: %v", err)
	}

	log.Printf("✅ Created user '%s' with auto-generated ID %d", username, userID)
	return userID, nil
}

// GetUserWalletAddress gets the wallet address for a user
func GetUserWalletAddress(db *sql.DB, userID int) (string, error) {
	var address string
	err := db.QueryRow("SELECT address FROM mmn_user_keys WHERE user_id = $1", userID).Scan(&address)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil // No wallet exists
		}
		return "", fmt.Errorf("failed to get wallet address: %v", err)
	}
	return address, nil
}

// GetTotalUsersWallet calculates the total wallet balance of all users except HRM
func GetTotalUsersWallet(db *sql.DB) (int64, error) {
	var total int64
	err := db.QueryRow("SELECT COALESCE(SUM(wallet), 0) FROM users WHERE username != $1", HRM_USERNAME).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate total users wallet: %v", err)
	}
	return total, nil
}

// GetHRMWalletInfo gets the HRM user's wallet information
func GetHRMWalletInfo(db *sql.DB) (int, string, error) {
	var userID int
	var address string

	// Get HRM user ID
	err := db.QueryRow("SELECT id FROM users WHERE username = $1", HRM_USERNAME).Scan(&userID)
	if err != nil {
		return 0, "", fmt.Errorf("failed to get HRM user ID: %v", err)
	}

	// Get HRM wallet address
	err = db.QueryRow("SELECT address FROM mmn_user_keys WHERE user_id = $1", userID).Scan(&address)
	if err != nil {
		return 0, "", fmt.Errorf("failed to get HRM wallet address: %v", err)
	}

	return userID, address, nil
}
