package main

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/holiman/uint256"
	clt "github.com/mezonai/mmn/client"
)

// Global nonce management
var (
	faucetNonce uint64
)

func defaultClient() (*clt.MmnClient, error) {
	cfg := clt.Config{Endpoint: "localhost:9001"}
	client, err := clt.NewClient(cfg)
	if err != nil {
		panic(err)
	}

	return client, err
}

// TransferTokens transfers tokens from faucet to a user wallet with retry mechanism
func TransferTokens(endpoint, faucetAddress, toAddress string, amount *uint256.Int, faucetPrivateKey ed25519.PrivateKey) error {
	ctx := context.Background()
	client, err := defaultClient()
	if err != nil {
		fmt.Printf("Failed to create client: %v", err)
		return err
	}

	// Get faucet account info to initialize nonce if needed
	if faucetNonce == 0 {
		var faucetAccount clt.Account
		faucetAccount, err = client.GetAccount(ctx, faucetAddress)
		if err != nil {
			return fmt.Errorf("failed to get faucet account: %w", err)
		}
		faucetNonce = faucetAccount.Nonce
		fmt.Printf("Faucet account - Balance: %d, Initial Nonce: %d\n", faucetAccount.Balance, faucetAccount.Nonce)
	}

	// Increment nonce for this transaction
	faucetNonce++

	unsigned, err := clt.BuildTransferTx(clt.TxTypeTransfer, faucetAddress, toAddress, amount, faucetNonce, uint64(time.Now().Unix()), "Migration transfer")
	if err != nil {
		fmt.Printf("Failed to build transfer tx: %v\n", err)
		return err
	}

	faucetSeed := faucetPrivateKey.Seed()
	signedRaw, err := clt.SignTx(unsigned, faucetSeed)
	if err != nil {
		fmt.Printf("Failed to sign tx: %v\n", err)
		return err
	}

	if !clt.Verify(unsigned, signedRaw.Sig) {
		fmt.Printf("Self verify failed\n")
		return err
	}

	res, err := client.AddTx(ctx, signedRaw)
	if err != nil {
		fmt.Printf("Failed to add tx: %v\n", err)
		return err
	}

	fmt.Printf("Transaction successful! Hash: %s\n", res.TxHash)

	return nil
}
