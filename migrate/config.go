package main

import (
	"os"
)

// Default configuration constants
const (
	defaultMainnetEndpoints = "localhost:9002" // Updated to use port 9002
	defaultDbURL            = "postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable"
	defaultMasterKey        = "bWV6b25fdGVzdF9tYXN0ZXJfa2V5XzEyMzQ1Njc4OTA=" // base64 of "mezon_test_master_key_1234567890"
)

// Configuration holds all configuration values
type Config struct {
	MMNEndpoint      string
	DatabaseURL      string
	MasterKey        string
	FaucetPrivateKey string
}

// LoadConfig loads configuration from environment variables with defaults
func LoadConfig() *Config {
	config := &Config{
		MMNEndpoint: getEnv("MMN_ENDPOINT", defaultMainnetEndpoints),
		DatabaseURL: getEnv("DATABASE_URL", defaultDbURL),
		MasterKey:   getEnv("MASTER_KEY", defaultMasterKey),
	}
	return config
}

// getEnv gets environment variable with fallback to default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
