package main

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"github.com/mezonai/mmn/logx"
	"time"

	"github.com/holiman/uint256"
	clt "github.com/mezonai/mmn/client"
)

func defaultClient() (*clt.MmnClient, error) {
	cfg := clt.Config{Endpoint: "localhost:9001"}
	client, err := clt.NewClient(cfg)
	if err != nil {
		panic(err)
	}

	return client, err
}

// TransferTokens transfers tokens from faucet to a user wallet with retry mechanism (legacy function)
func TransferTokens(faucetAddress, toAddress string, amount *uint256.Int, faucetPrivateKey ed25519.PrivateKey) error {
	ctx := context.Background()
	client, err := defaultClient()
	if err != nil {
		return fmt.Errorf("failed to create client: %v", err)
	}

	// Get fresh nonce for this transaction instead of using global counter
	var faucetAccount clt.Account
	faucetAccount, err = client.GetAccount(ctx, faucetAddress)
	if err != nil {
		return fmt.Errorf("failed to get faucet account for fresh nonce: %w", err)
	}

	currentNonce := faucetAccount.Nonce + 1

	unsigned, err := clt.BuildTransferTx(clt.TxTypeTransfer, faucetAddress, toAddress, amount, currentNonce, uint64(time.Now().Unix()), "Migration transfer", map[string]any{})
	if err != nil {
		return fmt.Errorf("failed to build transfer transaction: %v", err)
	}

	faucetSeed := faucetPrivateKey.Seed()
	signedRaw, err := clt.SignTx(unsigned, faucetSeed)
	if err != nil {
		return fmt.Errorf("failed to sign transaction: %v", err)
	}

	if !clt.Verify(unsigned, signedRaw.Sig) {
		return fmt.Errorf("transaction signature verification failed")
	}

	res, err := client.AddTx(ctx, signedRaw)
	if err != nil {
		return fmt.Errorf("failed to submit transaction: %v", err)
	}

	logx.Info("MIGRATE:TRANSFER TOKEN", "Transaction successful! Hash: ", res.TxHash)
	return nil
}

// GetAccountByAddress gets the account information for a given address
func GetAccountByAddress(address string) (clt.Account, error) {
	ctx := context.Background()
	client, err := defaultClient() // TODO: Make this configurable
	if err != nil {
		return clt.Account{}, fmt.Errorf("failed to create client: %v", err)
	}

	account, err := client.GetAccount(ctx, address)
	if err != nil {
		return clt.Account{}, fmt.Errorf("failed to get account: %w", err)
	}

	return account, nil
}
