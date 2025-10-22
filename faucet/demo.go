package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"log"
	"time"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/faucet"
	"github.com/mr-tron/base58"
)

func main() {
	fmt.Println("=== Multisig Faucet Demo ===")
	fmt.Println("This demo shows how to use the multisig faucet system")
	fmt.Println()

	// Step 1: Generate key pairs for 3 signers
	fmt.Println("1. Generating key pairs for 3 signers...")
	
	signers := make([]string, 3)
	privateKeys := make([][]byte, 3)
	
	for i := 0; i < 3; i++ {
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			log.Fatal("Failed to generate key pair:", err)
		}
		
		signers[i] = base58.Encode(pubKey)
		privateKeys[i] = privKey
		
		fmt.Printf("   Signer %d: %s\n", i+1, signers[i])
	}
	fmt.Println()

	// Step 2: Create multisig configuration (2-of-3)
	fmt.Println("2. Creating multisig configuration (2-of-3)...")
	
	config, err := faucet.CreateMultisigConfig(2, signers)
	if err != nil {
		log.Fatal("Failed to create multisig config:", err)
	}
	
	fmt.Printf("   Multisig Address: %s\n", config.Address)
	fmt.Printf("   Threshold: %d\n", config.Threshold)
	fmt.Printf("   Total Signers: %d\n", len(config.Signers))
	fmt.Println()

	// Step 3: Create faucet service
	fmt.Println("3. Creating faucet service...")
	
	maxAmount, _ := uint256.FromDecimal("1000000") // 1M tokens max
	cooldown := 1 * time.Hour
	service := faucet.NewMultisigFaucetService(maxAmount, cooldown)
	
	// Register the multisig configuration
	if err := service.RegisterMultisigConfig(config); err != nil {
		log.Fatal("Failed to register multisig config:", err)
	}
	
	fmt.Println("   Multisig configuration registered successfully")
	fmt.Println()

	// Step 4: Create a faucet request
	fmt.Println("4. Creating faucet request...")
	
	recipient := "recipient_address_here"
	amount, _ := uint256.FromDecimal("100000") // 100K tokens
	textData := "Demo faucet request"
	
	tx, err := service.CreateFaucetRequest(config.Address, recipient, amount, textData)
	if err != nil {
		log.Fatal("Failed to create faucet request:", err)
	}
	
	txHash := tx.Hash()
	fmt.Printf("   Faucet request created: %s\n", txHash)
	fmt.Printf("   Recipient: %s\n", tx.Recipient)
	fmt.Printf("   Amount: %s\n", tx.Amount.String())
	fmt.Printf("   Required signatures: %d\n", tx.Config.Threshold)
	fmt.Println()

	// Step 5: Add signatures (2 out of 3)
	fmt.Println("5. Adding signatures...")
	
	// First signature
	fmt.Println("   Adding signature from signer 1...")
	if err := service.AddSignature(txHash, signers[0], privateKeys[0]); err != nil {
		log.Fatal("Failed to add first signature:", err)
	}
	
	// Check status
	status, _ := service.GetTransactionStatus(txHash)
	fmt.Printf("   Status after first signature: %d/%d signatures\n", status.SignatureCount, status.RequiredCount)
	
	// Second signature
	fmt.Println("   Adding signature from signer 2...")
	if err := service.AddSignature(txHash, signers[1], privateKeys[1]); err != nil {
		log.Fatal("Failed to add second signature:", err)
	}
	
	// Check final status
	status, _ = service.GetTransactionStatus(txHash)
	fmt.Printf("   Status after second signature: %d/%d signatures\n", status.SignatureCount, status.RequiredCount)
	fmt.Printf("   Transaction complete: %t\n", status.IsComplete)
	fmt.Println()

	// Step 6: Execute the transaction
	fmt.Println("6. Executing transaction...")
	
	executedTx, err := service.VerifyAndExecute(txHash)
	if err != nil {
		log.Fatal("Failed to execute transaction:", err)
	}
	
	fmt.Printf("   Transaction executed successfully!\n")
	fmt.Printf("   Final signatures: %d\n", len(executedTx.Signatures))
	fmt.Printf("   Recipient: %s\n", executedTx.Recipient)
	fmt.Printf("   Amount: %s\n", executedTx.Amount.String())
	fmt.Println()

	// Step 7: Show service statistics
	fmt.Println("7. Service statistics...")
	
	stats := service.GetServiceStats()
	fmt.Printf("   Registered configs: %d\n", stats.RegisteredConfigs)
	fmt.Printf("   Pending transactions: %d\n", stats.PendingTransactions)
	fmt.Printf("   Max amount: %s\n", stats.MaxAmount.String())
	fmt.Printf("   Cooldown: %v\n", stats.Cooldown)
	fmt.Println()

	// Step 8: Demonstrate security features
	fmt.Println("8. Security demonstration...")
	
	// Try to add signature from unauthorized signer
	unauthorizedPubKey, unauthorizedPrivKey, _ := ed25519.GenerateKey(rand.Reader)
	fmt.Println("   Attempting to add signature from unauthorized signer...")
	err = service.AddSignature(txHash, base58.Encode(unauthorizedPubKey), unauthorizedPrivKey)
	if err != nil {
		fmt.Printf("   ✓ Correctly rejected unauthorized signer: %v\n", err)
	}
	
	// Try to create transaction with invalid amount
	fmt.Println("   Attempting to create transaction with excessive amount...")
	excessiveAmount, _ := uint256.FromDecimal("2000000") // 2M tokens (exceeds max)
	_, err = service.CreateFaucetRequest(config.Address, "another_recipient", excessiveAmount, "test")
	if err != nil {
		fmt.Printf("   ✓ Correctly rejected excessive amount: %v\n", err)
	}
	
	fmt.Println()

	fmt.Println("=== Demo completed successfully! ===")
	fmt.Println()
	fmt.Println("Key features demonstrated:")
	fmt.Println("✓ Multisig address generation")
	fmt.Println("✓ Transaction creation and signing")
	fmt.Println("✓ Signature verification")
	fmt.Println("✓ Transaction execution")
	fmt.Println("✓ Security validation")
	fmt.Println("✓ Service statistics")
	fmt.Println()
	fmt.Println("The multisig faucet system is now ready for production use!")
}
