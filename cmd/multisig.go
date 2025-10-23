package cmd

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mezonai/mmn/logx"
	pb "github.com/mezonai/mmn/proto"
	"github.com/mr-tron/base58"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type MultisigConfig struct {
	PrivateKeyFile string
	PrivateKey     string
	NodeURL        string
	Address        string
	MultisigAddr   string
	Recipient      string
	Amount         string
	Message        string
	TxHash         string
	Verbose        bool
}

var multisigConfig MultisigConfig

var multisigCmd = &cobra.Command{
	Use:   "multisig",
	Short: "Multisig faucet management commands",
	Long:  `Commands for managing multisig faucet operations including proposals, approvals, and whitelist management.`,
}

var addProposerCmd = &cobra.Command{
	Use:   "add-proposer",
	Short: "Add address to proposer whitelist",
	Long:  `Add an address to the proposer whitelist. Only approvers can add proposers.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := addProposer(multisigConfig); err != nil {
			logx.Error("MULTISIG CLI", err)
		}
	},
}

var addApproverCmd = &cobra.Command{
	Use:   "add-approver",
	Short: "Add address to approver whitelist",
	Long:  `Add an address to the approver whitelist. Only existing approvers can add new approvers.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := addApprover(multisigConfig); err != nil {
			logx.Error("MULTISIG CLI", err)
		}
	},
}

var createProposalCmd = &cobra.Command{
	Use:   "create-proposal",
	Short: "Create a new faucet proposal",
	Long:  `Create a new multisig faucet proposal. Only whitelisted proposers can create proposals.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := createProposal(multisigConfig); err != nil {
			logx.Error("MULTISIG CLI", err)
		}
	},
}

var getProposalsCmd = &cobra.Command{
	Use:   "get-proposals",
	Short: "Get list of pending proposals",
	Long:  `Get list of all pending multisig proposals.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := getProposals(multisigConfig); err != nil {
			logx.Error("MULTISIG CLI", err)
		}
	},
}

var approveCmd = &cobra.Command{
	Use:   "approve",
	Short: "Approve a proposal",
	Long:  `Add your signature to approve a multisig proposal.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := approveProposal(multisigConfig); err != nil {
			logx.Error("MULTISIG CLI", err)
		}
	},
}

var rejectCmd = &cobra.Command{
	Use:   "reject",
	Short: "Reject a proposal",
	Long:  `Reject a multisig proposal.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := rejectProposal(multisigConfig); err != nil {
			logx.Error("MULTISIG CLI", err)
		}
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check proposal status",
	Long:  `Check the status of a specific proposal.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := checkStatus(multisigConfig); err != nil {
			logx.Error("MULTISIG CLI", err)
		}
	},
}

var getMultisigAddressCmd = &cobra.Command{
	Use:   "get-multisig-address",
	Short: "Get multisig address from signers",
	Run: func(cmd *cobra.Command, args []string) {
		if err := getMultisigAddress(multisigConfig); err != nil {
			logx.Error("MULTISIG CLI", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(multisigCmd)
	multisigCmd.AddCommand(addProposerCmd)
	multisigCmd.AddCommand(addApproverCmd)
	multisigCmd.AddCommand(createProposalCmd)
	multisigCmd.AddCommand(getProposalsCmd)
	multisigCmd.AddCommand(approveCmd)
	multisigCmd.AddCommand(rejectCmd)
	multisigCmd.AddCommand(statusCmd)
	multisigCmd.AddCommand(getMultisigAddressCmd)

	multisigCmd.PersistentFlags().StringVarP(&multisigConfig.PrivateKeyFile, "private-key-file", "f", "", "private key file")
	multisigCmd.PersistentFlags().StringVarP(&multisigConfig.PrivateKey, "private-key", "p", "", "private key in hex")
	multisigCmd.PersistentFlags().StringVarP(&multisigConfig.NodeURL, "node-url", "u", "localhost:9001", "blockchain node URL")
	multisigCmd.PersistentFlags().BoolVarP(&multisigConfig.Verbose, "verbose", "v", false, "verbose output")

	addProposerCmd.Flags().StringVarP(&multisigConfig.Address, "address", "a", "", "address to add to proposer whitelist")
	addApproverCmd.Flags().StringVarP(&multisigConfig.Address, "address", "a", "", "address to add to approver whitelist")

	createProposalCmd.Flags().StringVarP(&multisigConfig.MultisigAddr, "multisig-addr", "m", "", "multisig address")
	createProposalCmd.Flags().StringVarP(&multisigConfig.Amount, "amount", "a", "", "amount to transfer")
	createProposalCmd.Flags().StringVar(&multisigConfig.Message, "message", "", "proposal message")

	approveCmd.Flags().StringVarP(&multisigConfig.TxHash, "tx-hash", "t", "", "transaction hash to approve")
	rejectCmd.Flags().StringVarP(&multisigConfig.TxHash, "tx-hash", "t", "", "transaction hash to reject")
	statusCmd.Flags().StringVarP(&multisigConfig.TxHash, "tx-hash", "t", "", "transaction hash to check status")
}

func createMultisigClient() (pb.TxServiceClient, error) {
	conn, err := grpc.Dial(multisigConfig.NodeURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to node: %w", err)
	}
	return pb.NewTxServiceClient(conn), nil
}

func loadPrivateKey() (ed25519.PrivateKey, string, error) {
	var privKeyStr string
	var err error

	if multisigConfig.PrivateKey != "" {
		privKeyStr = multisigConfig.PrivateKey
	} else if multisigConfig.PrivateKeyFile != "" {
		bytes, err := os.ReadFile(multisigConfig.PrivateKeyFile)
		if err != nil {
			return nil, "", err
		}
		privKeyStr = strings.TrimSpace(string(bytes))
	} else {
		return nil, "", fmt.Errorf("either --private-key or --private-key-file must be provided")
	}

	// Decode hex private key directly
	privBytes, err := hex.DecodeString(privKeyStr)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode hex private key: %w", err)
	}

	// Convert seed to private key
	privKey := ed25519.NewKeyFromSeed(privBytes)

	// Get public key from private key
	pubKey := privKey.Public().(ed25519.PublicKey)
	pubKeyStr := base58.Encode(pubKey)

	return privKey, pubKeyStr, nil
}

func signMessage(message string, privKey ed25519.PrivateKey) string {
	signature := ed25519.Sign(privKey, []byte(message))
	return hex.EncodeToString(signature)
}

func addProposer(config MultisigConfig) error {
	if config.Address == "" {
		return fmt.Errorf("--address is required")
	}

	client, err := createMultisigClient()
	if err != nil {
		return err
	}

	privKey, pubKeyStr, err := loadPrivateKey()
	if err != nil {
		return err
	}

	message := fmt.Sprintf("faucet_action:add_proposer")
	signature := signMessage(message, privKey)

	ctx := context.Background()
	resp, err := client.AddToProposerWhitelist(ctx, &pb.AddToProposerWhitelistRequest{
		Address:      config.Address,
		SignerPubkey: pubKeyStr,
		Signature:    signature,
	})

	if err != nil {
		return fmt.Errorf("failed to add proposer: %w", err)
	}

	if resp.Success {
		fmt.Printf("✅ Successfully added %s to proposer whitelist\n", config.Address)
	} else {
		return fmt.Errorf("failed to add proposer: %s", resp.Message)
	}

	return nil
}

func addApprover(config MultisigConfig) error {
	if config.Address == "" {
		return fmt.Errorf("--address is required")
	}

	client, err := createMultisigClient()
	if err != nil {
		return err
	}

	privKey, pubKeyStr, err := loadPrivateKey()
	if err != nil {
		return err
	}

	message := fmt.Sprintf("faucet_action:add_approver")
	signature := signMessage(message, privKey)

	ctx := context.Background()
	resp, err := client.AddToApproverWhitelist(ctx, &pb.AddToApproverWhitelistRequest{
		Address:      config.Address,
		SignerPubkey: pubKeyStr,
		Signature:    signature,
	})

	if err != nil {
		return fmt.Errorf("failed to add approver: %w", err)
	}

	if resp.Success {
		fmt.Printf("✅ Successfully added %s to approver whitelist\n", config.Address)
	} else {
		return fmt.Errorf("failed to add approver: %s", resp.Message)
	}

	return nil
}

func createProposal(config MultisigConfig) error {
	if config.MultisigAddr == "" {
		return fmt.Errorf("--multisig-addr is required")
	}
	if config.Amount == "" {
		return fmt.Errorf("--amount is required")
	}

	client, err := createMultisigClient()
	if err != nil {
		return err
	}

	privKey, pubKeyStr, err := loadPrivateKey()
	if err != nil {
		return err
	}

	message := fmt.Sprintf("faucet_action:create_faucet_request")
	signature := signMessage(message, privKey)

	ctx := context.Background()
	resp, err := client.CreateFaucetRequest(ctx, &pb.CreateFaucetRequestRequest{
		MultisigAddress: config.MultisigAddr,
		Amount:          config.Amount,
		TextData:        config.Message,
		SignerPubkey:    pubKeyStr,
		Signature:       signature,
	})

	if err != nil {
		return fmt.Errorf("failed to create proposal: %w", err)
	}

	if resp.Success {
		fmt.Printf("✅ Successfully created proposal: %s\n", resp.TxHash)
	} else {
		return fmt.Errorf("failed to create proposal: %s", resp.Message)
	}

	return nil
}

func getProposals(config MultisigConfig) error {
	client, err := createMultisigClient()
	if err != nil {
		return err
	}

	ctx := context.Background()
	resp, err := client.GetPendingTransactions(ctx, &pb.GetPendingTransactionsRequest{})
	if err != nil {
		return fmt.Errorf("failed to get proposals: %w", err)
	}

	fmt.Printf("📋 Found %d pending transactions:\n", resp.TotalCount)
	for i, tx := range resp.PendingTxs {
		fmt.Printf("  %d. Hash: %s, Status: %s\n", i+1, tx.TxHash, tx.Status.String())
	}

	return nil
}

func approveProposal(config MultisigConfig) error {
	if config.TxHash == "" {
		return fmt.Errorf("--tx-hash is required")
	}

	client, err := createMultisigClient()
	if err != nil {
		return err
	}

	privKey, pubKeyStr, err := loadPrivateKey()
	if err != nil {
		return err
	}

	message := fmt.Sprintf("faucet_action:add_signature")
	signature := signMessage(message, privKey)

	ctx := context.Background()
	resp, err := client.AddSignature(ctx, &pb.AddSignatureRequest{
		TxHash:       config.TxHash,
		SignerPubkey: pubKeyStr,
		Signature:    signature,
	})

	if err != nil {
		return fmt.Errorf("failed to approve proposal: %w", err)
	}

	if resp.Success {
		fmt.Printf("✅ Successfully approved proposal. Total signatures: %d\n", resp.SignatureCount)
	} else {
		return fmt.Errorf("failed to approve proposal: %s", resp.Message)
	}

	return nil
}

func rejectProposal(config MultisigConfig) error {
	return fmt.Errorf("reject functionality not implemented yet")
}

func checkStatus(config MultisigConfig) error {
	if config.TxHash == "" {
		return fmt.Errorf("--tx-hash is required")
	}

	client, err := createMultisigClient()
	if err != nil {
		return err
	}

	ctx := context.Background()
	resp, err := client.GetMultisigTransactionStatus(ctx, &pb.GetMultisigTransactionStatusRequest{
		TxHash: config.TxHash,
	})

	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	fmt.Printf("📊 Proposal Status:\n")
	fmt.Printf("  Status: %s\n", resp.Status)
	fmt.Printf("  Signatures: %d/%d\n", resp.SignatureCount, resp.RequiredSignatures)
	fmt.Printf("  Message: %s\n", resp.Message)

	return nil
}

func getMultisigAddress(config MultisigConfig) error {
	// Use the same signers as in docker-compose.yml
	signers := []string{
		"89y4uNijxzE9xXNvhU5oCbEN2RhSPCPQUwrJy7bPZPf8",
		"7oS1SxgwUTLTfxDqboRMdPYYBVxsc9xSq2Q2nM9VjssV",
		"S5MrFRhc92vmczcrDpNG8CLoDPh2jZH8f6Na11bFyDt",
	}

	// Sort signers like in CreateMultisigConfig
	sort.Strings(signers)

	// Generate multisig address using same logic as faucet/multisig.go
	configData := fmt.Sprintf("multisig:2:%s", signers[0])
	for i := 1; i < len(signers); i++ {
		configData += ":" + signers[i]
	}

	hash := sha256.Sum256([]byte(configData))
	address := base58.Encode(hash[:])

	fmt.Printf("🔑 Multisig Address: %s\n", address)
	fmt.Printf("📋 Signers: %v\n", signers)
	fmt.Printf("🔢 Threshold: 2\n")

	return nil
}
