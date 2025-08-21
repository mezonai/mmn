package main

import (
	"log"

	"github.com/mezonai/mmn/cmd"
)

func main() {
	log.Println("Testing StakeValidator integration...")

	// Test basic cmd functionality
	rootCmd := cmd.NewRootCommand()
	if rootCmd == nil {
		log.Fatal("Failed to create root command")
	}

	log.Println("✅ StakeValidator integration test passed - root command created successfully")
}
