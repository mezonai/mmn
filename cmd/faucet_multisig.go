package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/faucet"
	"github.com/mezonai/mmn/logx"
	"github.com/spf13/cobra"
)

var (
	// Multisig faucet service
	multisigFaucetService *faucet.MultisigFaucetService

	// Configuration flags
	multisigThreshold int
	multisigSigners   []string
	multisigAddress   string
	recipientAddress  string
	amount            string
	textData          string
	txHash            string
	signerPubKey      string
	privateKey        string
	maxAmount         *uint256.Int
	cooldown          string
)

// faucetMultisigCmd represents the multisig faucet command
var faucetMultisigCmd = &cobra.Command{
	Use:   "faucet-multisig",
	Short: "Multisig faucet management commands",
	Long:  `Commands for managing multisig faucet operations including configuration, transactions, and signatures.`,
}

// registerMultisigCmd represents the register multisig command
var registerMultisigCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a new multisig configuration",
	Long:  `Register a new multisig configuration with specified threshold and signers.`,
	Run: func(cmd *cobra.Command, args []string) {
		initMultisigFaucetService()

		config, err := faucet.CreateMultisigConfig(multisigThreshold, multisigSigners)
		if err != nil {
			logx.Error("RegisterMultisig", "failed to create config", err)
			os.Exit(1)
		}

		if err := multisigFaucetService.RegisterMultisigConfig(config); err != nil {
			logx.Error("RegisterMultisig", "failed to register config", err)
			os.Exit(1)
		}

		// Output configuration
		configJSON, _ := json.MarshalIndent(config, "", "  ")
		fmt.Printf("Multisig configuration registered successfully:\n%s\n", configJSON)
	},
}

// createFaucetCmd represents the create faucet command
var createFaucetCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new faucet request",
	Long:  `Create a new multisig faucet transaction request.`,
	Run: func(cmd *cobra.Command, args []string) {
		initMultisigFaucetService()

		// Parse amount
		amountUint, err := uint256.FromDecimal(amount)
		if err != nil {
			logx.Error("CreateFaucet", "invalid amount", err)
			os.Exit(1)
		}

		tx, err := multisigFaucetService.CreateFaucetRequest(multisigAddress, recipientAddress, amountUint, textData)
		if err != nil {
			logx.Error("CreateFaucet", "failed to create faucet request", err)
			os.Exit(1)
		}

		txHash := tx.Hash()
		fmt.Printf("Faucet request created successfully:\n")
		fmt.Printf("Transaction Hash: %s\n", txHash)
		fmt.Printf("Multisig Address: %s\n", tx.Sender)
		fmt.Printf("Recipient: %s\n", tx.Recipient)
		fmt.Printf("Amount: %s\n", tx.Amount.String())
		fmt.Printf("Required Signatures: %d\n", tx.Config.Threshold)
		fmt.Printf("Total Signers: %d\n", len(tx.Config.Signers))
	},
}

// addSignatureCmd represents the add signature command
var addSignatureCmd = &cobra.Command{
	Use:   "sign",
	Short: "Add signature to a multisig transaction",
	Long:  `Add a signature to a pending multisig transaction.`,
	Run: func(cmd *cobra.Command, args []string) {
		initMultisigFaucetService()

		// Decode private key (simplified - in production use proper key management)
		privKey := []byte(privateKey) // This should be properly decoded from base58

		if err := multisigFaucetService.AddSignature(txHash, signerPubKey, privKey); err != nil {
			logx.Error("AddSignature", "failed to add signature", err)
			os.Exit(1)
		}

		// Get updated transaction status
		tx, err := multisigFaucetService.GetPendingTransaction(txHash)
		if err != nil {
			logx.Error("AddSignature", "failed to get transaction status", err)
			os.Exit(1)
		}

		fmt.Printf("Signature added successfully:\n")
		fmt.Printf("Transaction Hash: %s\n", txHash)
		fmt.Printf("Signatures: %d/%d\n", tx.GetSignatureCount(), tx.GetRequiredSignatureCount())
		fmt.Printf("Is Complete: %t\n", tx.IsComplete())
	},
}

// executeTransactionCmd represents the execute transaction command
var executeTransactionCmd = &cobra.Command{
	Use:   "execute",
	Short: "Execute a verified multisig transaction",
	Long:  `Execute a multisig transaction that has been fully signed.`,
	Run: func(cmd *cobra.Command, args []string) {
		initMultisigFaucetService()

		tx, err := multisigFaucetService.VerifyAndExecute(txHash)
		if err != nil {
			logx.Error("ExecuteTransaction", "failed to execute transaction", err)
			os.Exit(1)
		}

		fmt.Printf("Transaction executed successfully:\n")
		fmt.Printf("Transaction Hash: %s\n", txHash)
		fmt.Printf("Recipient: %s\n", tx.Recipient)
		fmt.Printf("Amount: %s\n", tx.Amount.String())
		fmt.Printf("Signatures: %d\n", len(tx.Signatures))
	},
}

// getStatusCmd represents the get status command
var getStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Get transaction status",
	Long:  `Get the status of a multisig transaction.`,
	Run: func(cmd *cobra.Command, args []string) {
		initMultisigFaucetService()

		status, err := multisigFaucetService.GetTransactionStatus(txHash)
		if err != nil {
			logx.Error("GetStatus", "failed to get transaction status", err)
			os.Exit(1)
		}

		statusJSON, _ := json.MarshalIndent(status, "", "  ")
		fmt.Printf("Transaction Status:\n%s\n", statusJSON)
	},
}

// listPendingCmd represents the list pending command
var listPendingCmd = &cobra.Command{
	Use:   "list-pending",
	Short: "List all pending transactions",
	Long:  `List all pending multisig transactions.`,
	Run: func(cmd *cobra.Command, args []string) {
		initMultisigFaucetService()

		transactions := multisigFaucetService.ListPendingTransactions()

		fmt.Printf("Pending Transactions (%d):\n", len(transactions))
		for i, tx := range transactions {
			fmt.Printf("\n%d. Transaction Hash: %s\n", i+1, tx.Hash())
			fmt.Printf("   Recipient: %s\n", tx.Recipient)
			fmt.Printf("   Amount: %s\n", tx.Amount.String())
			fmt.Printf("   Signatures: %d/%d\n", tx.GetSignatureCount(), tx.GetRequiredSignatureCount())
			fmt.Printf("   Is Complete: %t\n", tx.IsComplete())
		}
	},
}

// getStatsCmd represents the get stats command
var getStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Get service statistics",
	Long:  `Get statistics about the multisig faucet service.`,
	Run: func(cmd *cobra.Command, args []string) {
		initMultisigFaucetService()

		stats := multisigFaucetService.GetServiceStats()
		statsJSON, _ := json.MarshalIndent(stats, "", "  ")
		fmt.Printf("Service Statistics:\n%s\n", statsJSON)
	},
}

func init() {
	// Add subcommands
	faucetMultisigCmd.AddCommand(registerMultisigCmd)
	faucetMultisigCmd.AddCommand(createFaucetCmd)
	faucetMultisigCmd.AddCommand(addSignatureCmd)
	faucetMultisigCmd.AddCommand(executeTransactionCmd)
	faucetMultisigCmd.AddCommand(getStatusCmd)
	faucetMultisigCmd.AddCommand(listPendingCmd)
	faucetMultisigCmd.AddCommand(getStatsCmd)

	// Register command flags
	registerMultisigCmd.Flags().IntVar(&multisigThreshold, "threshold", 2, "Number of required signatures (m in m-of-n)")
	registerMultisigCmd.Flags().StringSliceVar(&multisigSigners, "signers", []string{}, "List of signer public keys (comma-separated)")

	createFaucetCmd.Flags().StringVar(&multisigAddress, "multisig-address", "", "Multisig address")
	createFaucetCmd.Flags().StringVar(&recipientAddress, "recipient", "", "Recipient address")
	createFaucetCmd.Flags().StringVar(&amount, "amount", "", "Amount to transfer")
	createFaucetCmd.Flags().StringVar(&textData, "text-data", "", "Optional text data")

	addSignatureCmd.Flags().StringVar(&txHash, "tx-hash", "", "Transaction hash")
	addSignatureCmd.Flags().StringVar(&signerPubKey, "signer-pubkey", "", "Signer public key")
	addSignatureCmd.Flags().StringVar(&privateKey, "private-key", "", "Private key for signing")

	executeTransactionCmd.Flags().StringVar(&txHash, "tx-hash", "", "Transaction hash")

	getStatusCmd.Flags().StringVar(&txHash, "tx-hash", "", "Transaction hash")

	// Global flags
	faucetMultisigCmd.PersistentFlags().StringVar(&cooldown, "cooldown", "1h", "Cooldown period between requests")

	// Mark required flags
	registerMultisigCmd.MarkFlagRequired("signers")
	createFaucetCmd.MarkFlagRequired("multisig-address")
	createFaucetCmd.MarkFlagRequired("recipient")
	createFaucetCmd.MarkFlagRequired("amount")
	addSignatureCmd.MarkFlagRequired("tx-hash")
	addSignatureCmd.MarkFlagRequired("signer-pubkey")
	addSignatureCmd.MarkFlagRequired("private-key")
	executeTransactionCmd.MarkFlagRequired("tx-hash")
	getStatusCmd.MarkFlagRequired("tx-hash")
}

func initMultisigFaucetService() {
	if multisigFaucetService == nil {
		// Parse cooldown
		cooldownDuration := 1 * time.Hour // default
		if cooldownStr := cooldown; cooldownStr != "" {
			if duration, err := time.ParseDuration(cooldownStr); err == nil {
				cooldownDuration = duration
			}
		}

		// TODO: Create proper storage instance
		// For now, we'll use nil which will cause runtime errors
		// In production, you should create a proper storage instance
		multisigFaucetService = faucet.NewMultisigFaucetService(nil, maxAmount, cooldownDuration)
	}
}
