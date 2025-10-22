package faucet

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"log"
	"time"

	"github.com/holiman/uint256"
	"github.com/mr-tron/base58"
)

// ExampleMultisigFaucet demonstrates how to use the multisig faucet system
func ExampleMultisigFaucet() {
	fmt.Println("=== Multisig Faucet Example ===")

	// Step 1: Generate key pairs for 3 signers
	fmt.Println("\n1. Generating key pairs for 3 signers...")
	
	signers := make([]string, 3)
	privateKeys := make([][]byte, 3)
	
	for i := 0; i < 3; i++ {
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			log.Fatal("Failed to generate key pair:", err)
		}
		
		signers[i] = base58.Encode(pubKey)
		privateKeys[i] = privKey
		
		fmt.Printf("Signer %d: %s\n", i+1, signers[i])
	}

	// Step 2: Create multisig configuration (2-of-3)
	fmt.Println("\n2. Creating multisig configuration (2-of-3)...")
	
	config, err := CreateMultisigConfig(2, signers)
	if err != nil {
		log.Fatal("Failed to create multisig config:", err)
	}
	
	fmt.Printf("Multisig Address: %s\n", config.Address)
	fmt.Printf("Threshold: %d\n", config.Threshold)
	fmt.Printf("Total Signers: %d\n", len(config.Signers))

	// Step 3: Create faucet service
	fmt.Println("\n3. Creating faucet service...")
	
	maxAmount, _ := uint256.FromDecimal("1000000") // 1M tokens max
	cooldown := 1 * time.Hour
	service := NewMultisigFaucetService(maxAmount, cooldown)
	
	// Register the multisig configuration
	if err := service.RegisterMultisigConfig(config); err != nil {
		log.Fatal("Failed to register multisig config:", err)
	}
	
	fmt.Println("Multisig configuration registered successfully")

	// Step 4: Create a faucet request
	fmt.Println("\n4. Creating faucet request...")
	
	recipient := "recipient_address_here"
	amount, _ := uint256.FromDecimal("100000") // 100K tokens
	textData := "Test faucet request"
	
	tx, err := service.CreateFaucetRequest(config.Address, recipient, amount, textData)
	if err != nil {
		log.Fatal("Failed to create faucet request:", err)
	}
	
	txHash := tx.Hash()
	fmt.Printf("Faucet request created: %s\n", txHash)
	fmt.Printf("Recipient: %s\n", tx.Recipient)
	fmt.Printf("Amount: %s\n", tx.Amount.String())
	fmt.Printf("Required signatures: %d\n", tx.Config.Threshold)

	// Step 5: Add signatures (2 out of 3)
	fmt.Println("\n5. Adding signatures...")
	
	// First signature
	fmt.Println("Adding signature from signer 1...")
	if err := service.AddSignature(txHash, signers[0], privateKeys[0]); err != nil {
		log.Fatal("Failed to add first signature:", err)
	}
	
	// Check status
	status, _ := service.GetTransactionStatus(txHash)
	fmt.Printf("Status after first signature: %d/%d signatures\n", status.SignatureCount, status.RequiredCount)
	
	// Second signature
	fmt.Println("Adding signature from signer 2...")
	if err := service.AddSignature(txHash, signers[1], privateKeys[1]); err != nil {
		log.Fatal("Failed to add second signature:", err)
	}
	
	// Check final status
	status, _ = service.GetTransactionStatus(txHash)
	fmt.Printf("Status after second signature: %d/%d signatures\n", status.SignatureCount, status.RequiredCount)
	fmt.Printf("Transaction complete: %t\n", status.IsComplete)

	// Step 6: Execute the transaction
	fmt.Println("\n6. Executing transaction...")
	
	executedTx, err := service.VerifyAndExecute(txHash)
	if err != nil {
		log.Fatal("Failed to execute transaction:", err)
	}
	
	fmt.Printf("Transaction executed successfully!\n")
	fmt.Printf("Final signatures: %d\n", len(executedTx.Signatures))
	fmt.Printf("Recipient: %s\n", executedTx.Recipient)
	fmt.Printf("Amount: %s\n", executedTx.Amount.String())

	// Step 7: Show service statistics
	fmt.Println("\n7. Service statistics...")
	
	stats := service.GetServiceStats()
	fmt.Printf("Registered configs: %d\n", stats.RegisteredConfigs)
	fmt.Printf("Pending transactions: %d\n", stats.PendingTransactions)
	fmt.Printf("Max amount: %s\n", stats.MaxAmount.String())
	fmt.Printf("Cooldown: %v\n", stats.Cooldown)

	fmt.Println("\n=== Example completed successfully! ===")
}

// ExampleMultisigSecurity demonstrates security features
func ExampleMultisigSecurity() {
	fmt.Println("\n=== Multisig Security Example ===")

	// Test invalid threshold
	fmt.Println("\n1. Testing invalid threshold...")
	_, err := CreateMultisigConfig(5, []string{"key1", "key2"}) // 5-of-2 (invalid)
	if err != nil {
		fmt.Printf("✓ Correctly rejected invalid threshold: %v\n", err)
	}

	// Test insufficient signers
	fmt.Println("\n2. Testing insufficient signers...")
	_, err = CreateMultisigConfig(2, []string{"key1"}) // 2-of-1 (invalid)
	if err != nil {
		fmt.Printf("✓ Correctly rejected insufficient signers: %v\n", err)
	}

	// Test duplicate signers
	fmt.Println("\n3. Testing duplicate signers...")
	_, err = CreateMultisigConfig(2, []string{"key1", "key1"}) // duplicate keys
	if err != nil {
		fmt.Printf("✓ Correctly rejected duplicate signers: %v\n", err)
	}

	fmt.Println("\n=== Security tests completed! ===")
}

// ExampleMultisigWorkflow demonstrates a complete workflow
func ExampleMultisigWorkflow() {
	fmt.Println("\n=== Complete Multisig Workflow ===")

	// This would be the typical workflow in a real application:
	// 1. Admin creates multisig configuration
	// 2. Configuration is registered with the service
	// 3. Users request faucet funds
	// 4. Signers review and sign transactions
	// 5. When threshold is reached, transaction is executed
	// 6. Funds are transferred to recipient

	fmt.Println("Workflow steps:")
	fmt.Println("1. Create multisig configuration (m-of-n)")
	fmt.Println("2. Register configuration with faucet service")
	fmt.Println("3. Create faucet request (recipient, amount)")
	fmt.Println("4. Signers add their signatures")
	fmt.Println("5. When threshold reached, execute transaction")
	fmt.Println("6. Funds transferred to recipient")

	fmt.Println("\nSecurity features:")
	fmt.Println("- Multiple signatures required")
	fmt.Println("- Configurable threshold (m-of-n)")
	fmt.Println("- Signature verification")
	fmt.Println("- Transaction validation")
	fmt.Println("- Cooldown periods")
	fmt.Println("- Amount limits")

	fmt.Println("\n=== Workflow example completed! ===")
}
