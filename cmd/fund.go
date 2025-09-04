package cmd

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/proto"
	"io"
	"os"
	"strings"
	"time"

	"github.com/holiman/uint256"
	mmn "github.com/mezonai/mmn/client"
	"github.com/mr-tron/base58"
	"github.com/spf13/cobra"
)

var fundingConfig struct {
	PrivateKey     string
	PrivateKeyFile string
	NodeURL        string
	TextData       string
	Verbose        bool
}

// fundCmd represents the transfer command
var fundCmd = &cobra.Command{
	Use:   "fund <recipient> <amount> [flags]",
	Short: "Fund an account with tokens from the faucet",
	Long: `This command sends tokens from the faucet account to the specified recipient address.
The faucet private key can be provided either directly via --private-key flag
or via a file using --private-key-file flag.

Examples:
  # Fund account with 1000 tokens using private key file
  fund 5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY 1_000 --private-key-file /path/to/key.txt

  # Fund account with 500 tokens using private key directly
  fund 5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY 500 --private-key "your-private-key-here"`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		recipient := args[0]
		amountStr := args[1]

		// Parse amount string to uint256.Int
		amount, err := uint256.FromDecimal(strings.ReplaceAll(amountStr, "_", ""))
		if err != nil {
			logx.Error("FAUCET FUNDING", "could not parse amount string: ", err)
			return
		}

		if err := fundAccount(recipient, amount); err != nil {
			logx.Error("FAUCET FUNDING", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(fundCmd)

	fundCmd.PersistentFlags().StringVarP(&fundingConfig.PrivateKeyFile, "private-key-file", "f", "", "Faucet private key file in hex")
	fundCmd.PersistentFlags().StringVarP(&fundingConfig.PrivateKey, "private-key", "p", "", "Faucet private key in hex")
	fundCmd.PersistentFlags().StringVarP(&fundingConfig.NodeURL, "node-url", "u", "localhost:9001", "Blockchain node URL")
	fundCmd.PersistentFlags().StringVarP(&fundingConfig.TextData, "text-data", "t", "Funding at "+time.Now().Format(time.DateTime), "Blockchain node URL")
	fundCmd.PersistentFlags().BoolVarP(&fundingConfig.Verbose, "verbose", "v", false, "Verbose output")
}

func fundAccount(recipient string, amount *uint256.Int) error {
	// Load faucet private key
	if fundingConfig.Verbose {
		logx.Debug("FAUCET FUNDING", "Loading faucet private key...")
	}
	privKeyStr, err := loadFaucetPrivateKey()
	if err != nil {
		return fmt.Errorf("failed to load faucet private key: %w", err)
	}

	// Convert private key string to ed25519 private key and get faucet address
	faucetAddress, faucetPrivateKey, err := parsePrivateKey(privKeyStr)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	// Create blockchain grpc client connection
	grpcClient, err := createClient()
	if err != nil {
		return fmt.Errorf("failed to create grpc client: %w", err)
	}
	defer grpcClient.Close()

	// Get faucet account info to get current nonce
	ctx := context.Background()
	faucetAccount, err := grpcClient.GetAccount(ctx, faucetAddress)
	if err != nil {
		return fmt.Errorf("failed to get faucet account: %w", err)
	}

	// Build and sign transfer transaction
	nonce := faucetAccount.Nonce + 1
	unsigned, err := mmn.BuildTransferTx(
		mmn.TxTypeTransfer,
		faucetAddress,
		recipient,
		amount,
		nonce,
		uint64(time.Now().Unix()),
		fundingConfig.TextData,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to build transfer transaction: %w", err)
	}

	// Sign the transaction
	signedTx, err := mmn.SignTx(unsigned, faucetPrivateKey.Seed())
	if err != nil {
		return fmt.Errorf("failed to sign transaction: %w", err)
	}

	// Send transaction to blockchain
	if fundingConfig.Verbose {
		logx.Debug("FAUCET FUNDING", fmt.Sprintf("Sending funding transaction to %s: %+v", fundingConfig.NodeURL, unsigned))
	}
	addTxResp, err := grpcClient.AddTx(ctx, signedTx)
	if err != nil {
		return fmt.Errorf("failed to send transaction: %w", err)
	}
	if !addTxResp.Ok {
		return fmt.Errorf("failed to send transaction: %s", addTxResp.Error)
	}
	if fundingConfig.Verbose {
		logx.Debug("FAUCET FUNDING", "Funding transaction sent: ", addTxResp.TxHash)
	}

	// Track for transaction updates
	statusStream, err := grpcClient.SubscribeTransactionStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to subscribe transaction status: %w", err)
	}
	for {
		// Receive updates from stream
		update, err := statusStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to receive transaction status: %w", err)
		}

		// Ignore other transaction updates
		if update.TxHash != addTxResp.TxHash {
			continue
		}
		if fundingConfig.Verbose {
			logx.Debug("FAUCET FUNDING", "Transaction status received: ", update.Status)
		}
		if update.Status == proto.TransactionStatus_FAILED || update.Status == proto.TransactionStatus_FINALIZED {
			logx.Info("FAUCET FUNDING", "Transaction ended with status: ", update.Status)
			break
		}
	}

	// Print recipient wallet state after transfer
	recipientAccount, err := grpcClient.GetAccount(ctx, recipient)
	if err != nil {
		return fmt.Errorf("failed to get recipient account: %w", err)
	}
	logx.Info("FAUCET FUNDING", "Recipient account after transfer: ", recipientAccount)

	return nil
}

// loadFaucetPrivateKey loads the key from config, which is set by command flags
// the private key is originally in hex format
func loadFaucetPrivateKey() (string, error) {
	if fundingConfig.PrivateKey != "" {
		return fundingConfig.PrivateKey, nil
	}
	bytes, err := os.ReadFile(fundingConfig.PrivateKeyFile)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// parsePrivateKey converts a private key string to ed25519 private key and returns the faucet address
func parsePrivateKey(privKeyStr string) (faucetAddress string, faucetPrivate ed25519.PrivateKey, err error) {
	privBytes, err := hex.DecodeString(strings.TrimSpace(privKeyStr))
	if err != nil {
		return
	}

	faucetPrivate = ed25519.NewKeyFromSeed(privBytes[len(privBytes)-ed25519.SeedSize:])
	faucetPubKey := faucetPrivate.Public().(ed25519.PublicKey)
	faucetAddress = base58.Encode(faucetPubKey)

	return
}

// createClient creates a new blockchain client connection
func createClient() (*mmn.MmnClient, error) {
	cfg := mmn.Config{Endpoint: fundingConfig.NodeURL}
	client, err := mmn.NewClient(cfg)
	if err != nil {
		return nil, logx.Errorf("failed to create grpc client: %w", err)
	}
	return client, nil
}
