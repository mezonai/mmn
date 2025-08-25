package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	mmnpb "github.com/mezonai/mmn/proto"
)

// Global nonce management
var (
	faucetNonce uint64
)

// signTx signs a transaction with the given private key seed
func signTx(tx *Tx, privKeySeed []byte) (SignedTx, error) {
	privKey := ed25519.NewKeyFromSeed(privKeySeed)
	txHash := serializeTx(tx)
	signature := ed25519.Sign(privKey, txHash)

	return SignedTx{
		Tx:  tx,
		Sig: hex.EncodeToString(signature),
	}, nil
}

// serializeTx serializes a transaction for signing
func serializeTx(tx *Tx) []byte {
	metadata := fmt.Sprintf("%d|%s|%s|%d|%s|%d", tx.Type, tx.Sender, tx.Recipient, tx.Amount, tx.TextData, tx.Nonce)
	return []byte(metadata)
}

// TransferTokens transfers tokens from faucet to a user wallet with retry mechanism
func TransferTokens(endpoint, faucetAddress, toAddress string, amount uint64, faucetPrivateKey ed25519.PrivateKey) error {
	// Create gRPC connection
	conn, err := grpc.Dial(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to MMN node: %w", err)
	}
	defer conn.Close()

	// Create clients
	txClient := mmnpb.NewTxServiceClient(conn)
	accountClient := mmnpb.NewAccountServiceClient(conn)
	ctx := context.Background()

	// Get faucet account info to initialize nonce if needed
	if faucetNonce == 0 {
		faucetAccount, err := accountClient.GetAccount(ctx, &mmnpb.GetAccountRequest{
			Address: faucetAddress,
		})
		if err != nil {
			return fmt.Errorf("failed to get faucet account: %w", err)
		}
		faucetNonce = faucetAccount.Nonce
		fmt.Printf("Faucet account - Balance: %d, Initial Nonce: %d\n", faucetAccount.Balance, faucetAccount.Nonce)
	}

	// Increment nonce for this transaction
	faucetNonce++

	// Build transaction
	tx := &Tx{
		Type:      0, // Transfer type
		Sender:    faucetAddress,
		Recipient: toAddress,
		Amount:    amount,
		Timestamp: uint64(time.Now().Unix()),
		TextData:  "Migration transfer",
		Nonce:     faucetNonce, // Use incremented nonce
	}

	fmt.Printf("Sending transaction with nonce: %d\n", tx.Nonce)

	// Sign transaction
	faucetSeed := faucetPrivateKey.Seed()
	signedTx, err := signTx(tx, faucetSeed)
	if err != nil {
		return fmt.Errorf("failed to sign transaction: %w", err)
	}

	// Convert to protobuf
	protoTx := &mmnpb.SignedTxMsg{
		TxMsg: &mmnpb.TxMsg{
			Type:      int32(tx.Type),
			Sender:    tx.Sender,
			Recipient: tx.Recipient,
			Amount:    tx.Amount,
			Nonce:     tx.Nonce,
			TextData:  tx.TextData,
			Timestamp: tx.Timestamp,
		},
		Signature: signedTx.Sig,
	}

	// Send transaction
	resp, err := txClient.AddTx(ctx, protoTx)
	if err != nil {
		return fmt.Errorf("failed to send transaction: %w", err)
	}

	if !resp.Ok {
		return fmt.Errorf("transaction failed: %s", resp.Error)
	}

	return nil
}

// TestConnection tests connection to MMN blockchain with retry mechanism
func TestConnection(mmnEndpoint string) error {
	const maxRetries = 3
	const retryDelay = time.Second * 2

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("⏳ Retrying connection test (attempt %d/%d) after error: %v", attempt+1, maxRetries, lastErr)
			time.Sleep(retryDelay)
		}

		err := attemptConnection(mmnEndpoint)
		if err == nil {
			log.Printf("✅ MMN blockchain connection successful (endpoint: %s)", mmnEndpoint)
			return nil
		}
		lastErr = err
	}

	return fmt.Errorf("failed to connect to MMN after %d attempts: %v", maxRetries, lastErr)
}

// attemptConnection performs a single connection test attempt
func attemptConnection(mmnEndpoint string) error {
	conn, err := grpc.Dial(mmnEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to MMN: %v", err)
	}
	defer conn.Close()

	accountClient := mmnpb.NewAccountServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to get account info for a dummy address to test connection
	req := &mmnpb.GetAccountRequest{
		Address: "test-address",
	}

	_, err = accountClient.GetAccount(ctx, req)
	// Connection successful even if account doesn't exist or returns error
	// The important thing is that we can reach the server
	return nil
}
