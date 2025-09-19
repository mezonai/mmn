package cmd

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/logx"

	mmn "github.com/mezonai/mmn/client"
	"github.com/mr-tron/base58"
	"github.com/spf13/cobra"
)

type TransferConfig struct {
	PrivateKey     string
	PrivateKeyFile string
	NodeURL        string
	To             string
	Amount         string
	Message        string
	Verbose        bool
}

var transferConfig TransferConfig

// transferCmd represents the transfer command
var transferCmd = &cobra.Command{
	Use:   "transfer [flags]",
	Short: "Transfer token to another account",
	Long: `This command sends tokens from the account to the specified recipient address.
The private key can be provided either directly via --private-key flag
or via a file using --private-key-file flag.

Examples:
  # Transfer 1000 tokens using private key file
  transfer -t 5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY -a 1_000 -f /path/to/key.txt

  # Transfer 500 tokens using private key directly
  transfer -t 5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY -a 500 -p "your-private-key-here"`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := transferToken(transferConfig); err != nil {
			logx.Error("TRANSFER CLI", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(transferCmd)

	transferCmd.PersistentFlags().StringVarP(&transferConfig.PrivateKeyFile, "private-key-file", "f", "", "sender private key file")
	transferCmd.PersistentFlags().StringVarP(&transferConfig.PrivateKey, "private-key", "p", "", "sender private key in hex")
	transferCmd.PersistentFlags().StringVarP(&transferConfig.NodeURL, "node-url", "u", "localhost:9001", "blockchain node URL")
	transferCmd.PersistentFlags().StringVarP(&transferConfig.To, "to", "t", "", "address of recipient")
	transferCmd.PersistentFlags().StringVarP(&transferConfig.Amount, "amount", "a", "", "amount")
	transferCmd.PersistentFlags().StringVarP(&transferConfig.Message, "message", "m", "Funding at "+time.Now().Format(time.DateTime), "transfer text data")
	transferCmd.PersistentFlags().BoolVarP(&transferConfig.Verbose, "verbose", "v", false, "verbose output")
}

func transferToken(transferConfig TransferConfig) error {
	// Parse the amount string to uint256.Int
	amount, err := uint256.FromDecimal(strings.ReplaceAll(transferConfig.Amount, "_", ""))
	if err != nil {
		return fmt.Errorf("could not parse amount string: %v", err)
	}

	// Load sender private key
	if transferConfig.Verbose {
		logx.Debug("TRANSFER CLI", "Loading sender private key...")
	}
	privKeyStr, err := loadSenderPrivateKey(transferConfig)
	if err != nil {
		return fmt.Errorf("failed to load sender private key: %w", err)
	}

	// Convert private key string to ed25519 private key and get sender address
	senderAddress, senderPrivateKey, err := parsePrivateKey(privKeyStr)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	// Create blockchain grpc client connection
	grpcClient, err := createClient()
	if err != nil {
		return fmt.Errorf("failed to create grpc client: %w", err)
	}
	defer grpcClient.Close()

	// Get sender account info to get current nonce
	ctx := context.Background()
	senderAccount, err := grpcClient.GetAccount(ctx, senderAddress)
	if err != nil {
		return fmt.Errorf("failed to get sender account: %w", err)
	}

	// Build and sign transferConfig transaction
	nonce := senderAccount.Nonce + 1
	unsigned, err := mmn.BuildTransferTx(
		mmn.TxTypeFaucet,
		senderAddress,
		transferConfig.To,
		amount,
		nonce,
		uint64(time.Now().Unix()),
		transferConfig.Message,
		nil,
		"",
		"",
	)
	if err != nil {
		return fmt.Errorf("failed to build transferConfig transaction: %w", err)
	}

	// Sign the transaction
	signedTx, err := mmn.SignTx(unsigned, senderPrivateKey.Seed())
	if err != nil {
		return fmt.Errorf("failed to sign transaction: %w", err)
	}

	// Send transaction to blockchain
	if transferConfig.Verbose {
		logx.Debug("TRANSFER CLI", fmt.Sprintf("Sending funding transaction to %s: %+v", transferConfig.NodeURL, unsigned))
	}
	addTxResp, err := grpcClient.AddTx(ctx, signedTx)
	if err != nil {
		return fmt.Errorf("failed to send transaction: %w", err)
	}
	if !addTxResp.Ok {
		return fmt.Errorf("failed to send transaction: %s", addTxResp.Error)
	}
	if transferConfig.Verbose {
		logx.Debug("TRANSFER CLI", "Funding transaction sent: ", addTxResp.TxHash)
	}

	// Track for transaction updates
	logx.Debug("TRANSFER CLI", "Waiting for transaction to be processed...")
	time.Sleep(4 * time.Second)
	txInfo, err := grpcClient.GetTxByHash(ctx, addTxResp.TxHash)
	if err != nil {
		return fmt.Errorf("failed to get transaction status: %w", err)
	}
	if txInfo.ErrMsg != "" {
		return fmt.Errorf("funding error: %s", txInfo.ErrMsg)
	}

	// Print recipient wallet state after transferConfig
	recipientAccount, err := grpcClient.GetAccount(ctx, transferConfig.To)
	if err != nil {
		return fmt.Errorf("failed to get recipient account: %w", err)
	}
	logx.Info("TRANSFER CLI", "Recipient account after transferConfig: ", recipientAccount)

	return nil
}

// loadSenderPrivateKey loads the key from config, which is set by command flags
// the private key is originally in hex format
func loadSenderPrivateKey(transferConfig TransferConfig) (string, error) {
	fmt.Printf("%+v\n", transferConfig)
	if transferConfig.PrivateKey != "" {
		return transferConfig.PrivateKey, nil
	}
	bytes, err := os.ReadFile(transferConfig.PrivateKeyFile)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// parsePrivateKey converts a private key string to ed25519 private key and returns the sender address
func parsePrivateKey(privKeyStr string) (address string, private ed25519.PrivateKey, err error) {
	privBytes, err := hex.DecodeString(strings.TrimSpace(privKeyStr))
	if err != nil {
		return
	}

	private = ed25519.NewKeyFromSeed(privBytes[len(privBytes)-ed25519.SeedSize:])
	pubKey := private.Public().(ed25519.PublicKey)
	address = base58.Encode(pubKey)

	return
}

// createClient creates a new blockchain client connection
func createClient() (*mmn.MmnClient, error) {
	cfg := mmn.Config{Endpoint: transferConfig.NodeURL}
	client, err := mmn.NewClient(cfg)
	if err != nil {
		return nil, logx.Errorf("failed to create grpc client: %w", err)
	}
	return client, nil
}
