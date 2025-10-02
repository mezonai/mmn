package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/mezonai/mmn/logx"
)

func ConnectDatabase(databaseURL string) (*sql.DB, error) {
	const maxRetries = 5
	const retryDelay = time.Second * 3

	var db *sql.DB
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			logx.Info("DATABASE", fmt.Sprintf("Retrying database connection (attempt %d/%d) after error: %v", attempt+1, maxRetries, lastErr))
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
		logx.Info("DATABASE", "Database connection established successfully")
		return db, nil
	}

	return nil, fmt.Errorf("failed to connect to database after %d attempts: %v", maxRetries, lastErr)
}

func GetUsers(db *sql.DB) ([]map[string]interface{}, error) {
	rows, err := db.Query("SELECT id, username, wallet FROM users where wallet is not null and wallet > 0")
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %v", err)
	}
	defer rows.Close()

	var users []map[string]interface{}
	for rows.Next() {
		var id int64
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
