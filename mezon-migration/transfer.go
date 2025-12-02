package main

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/mezonai/mmn/logx"

	"github.com/holiman/uint256"
	clt "github.com/mezonai/mmn/client"
)

func NewMmnClient(endpoint string) (*clt.MmnClient, error) {
	cfg := clt.Config{Endpoint: endpoint}
	client, err := clt.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return client, err
}

func TransferTokens(client *clt.MmnClient, ctx context.Context, faucetAddress, toAddress string, amount *uint256.Int, faucetPrivateKey ed25519.PrivateKey, nonce uint64, textData string) error {
	unsigned, err := clt.BuildTransferTx(clt.TxTypeTransferByKey, faucetAddress, toAddress, amount, nonce, uint64(time.Now().Unix()), textData, map[string]string{}, "", "")
	if err != nil {
		return fmt.Errorf("failed to build transfer transaction: %v", err)
	}

	faucetSeed := faucetPrivateKey.Seed()
	signedRaw, err := clt.SignTx(unsigned, []byte{}, faucetSeed)
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

	logx.Info("TRANSFER", "Transaction successful! Hash: ", res.TxHash)
	return nil
}

func GetAccountByAddress(client *clt.MmnClient, ctx context.Context, address string) (clt.Account, error) {
	account, err := client.GetAccount(ctx, address)
	if err != nil {
		return clt.Account{}, fmt.Errorf("failed to get account: %w", err)
	}

	return account, nil
}
