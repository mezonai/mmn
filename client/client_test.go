package client

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/holiman/uint256"
	mmnpb "github.com/mezonai/mmn/proto"
	"github.com/mr-tron/base58"
)

const faucetAddress = "tCpERuK8HdBMFVShya49pgfBFVyxbzzgDp7EKKE2Nx6"

func defaultClient() (*MmnClient, error) {
	cfg := Config{Endpoint: "localhost:9001"}
	client, err := NewClient(cfg)
	if err != nil {
		panic(err)
	}

	return client, err
}

func getFaucetAccount() (string, ed25519.PrivateKey) {
	fmt.Println("getFaucetAccount")
	faucetPrivateKeyHex := "302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee"
	faucetPrivateKeyDer, err := hex.DecodeString(faucetPrivateKeyHex)
	if err != nil {
		fmt.Println("err", err)
		panic(err)
	}
	fmt.Println("faucetPrivateKeyDer")

	// Extract the last 32 bytes as the Ed25519 seed
	faucetSeed := faucetPrivateKeyDer[len(faucetPrivateKeyDer)-32:]
	faucetPrivateKey := ed25519.NewKeyFromSeed(faucetSeed)
	faucetPublicKey := faucetPrivateKey.Public().(ed25519.PublicKey)
	faucetPublicKeyBase58 := base58.Encode(faucetPublicKey[:])
	fmt.Println("faucetPublicKeyBase58", faucetPublicKeyBase58)
	return faucetPublicKeyBase58, faucetPrivateKey
}

func TestClient_Config(t *testing.T) {
	client, err := defaultClient()
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	if client.cfg.Endpoint != "localhost:9001" {
		t.Errorf("Client config endpoint = %v, want %v", client.cfg.Endpoint, "localhost:9001")
	}
}

func TestClient_CheckHealth(t *testing.T) {
	client, err := defaultClient()
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	resp, err := client.CheckHealth(context.Background())
	if err != nil {
		t.Fatalf("CheckHealth() error = %v", err)
	}

	if resp == nil {
		t.Error("CheckHealth() returned nil response")
		return
	}
	if resp.ErrorMessage != "" {
		t.Fatalf("Error Message: %s", resp.ErrorMessage)
	}

	// Basic validation
	if resp.NodeId == "" {
		t.Fatalf("Expected non-empty node ID from: %s", client.cfg.Endpoint)
	}

	if resp.Status == mmnpb.HealthCheckResponse_UNKNOWN {
		t.Fatalf("Warning: Node %s returned UNKNOWN status", client.cfg.Endpoint)
	}

	if resp.Status == mmnpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("Warning: Node %s is NOT_SERVING", client.cfg.Endpoint)
	}

	if resp.CurrentSlot <= 0 {
		t.Fatalf("Warning: Node %s returned invalid current slot: %d", client.cfg.Endpoint, resp.CurrentSlot)
	}

	// Log health check response in one line
	t.Logf("Health Check - Status: %v, Node ID: %s, Slot: %d, Height: %d, Mempool: %d, Leader: %v, Follower: %v, Version: %s, Uptime: %ds",
		resp.Status, resp.NodeId, resp.CurrentSlot, resp.BlockHeight, resp.MempoolSize, resp.IsLeader, resp.IsFollower, resp.Version, resp.Uptime)
}

func TestClient_FaucetSendToken(t *testing.T) {
	client, err := defaultClient()
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()

	faucetPublicKey, faucetPrivateKey := getFaucetAccount()
	fmt.Println("faucetPublicKey", faucetPublicKey)
	toAddress := "PqbfV5pLGCBSkUcCgNL6L3JTQBwFEFtos3ADhT4FVPP" // dummy base58 for test

	// Get current faucet account to get the next nonce
	faucetAccount, err := client.GetAccount(ctx, faucetPublicKey)
	if err != nil {
		t.Fatalf("Failed to get faucet account: %v", err)
	}
	nextNonce := faucetAccount.Nonce + 1
	t.Logf("Faucet account nonce: %d, using next nonce: %d", faucetAccount.Nonce, nextNonce)

	// Extract the seed from the private key (first 32 bytes)
	faucetPrivateKeySeed := faucetPrivateKey.Seed()
	transferType := TxTypeTransfer
	fromAddr := faucetPublicKey
	toAddr := toAddress
	amount := uint256.NewInt(1)
	nonce := nextNonce
	textData := "Integration test transfer"

	unsigned, err := BuildTransferTx(transferType, fromAddr, toAddr, amount, nonce, uint64(time.Now().Unix()), textData)
	if err != nil {
		t.Fatalf("Failed to build transfer tx: %v", err)
	}

	signedRaw, err := SignTx(unsigned, faucetPrivateKeySeed)
	if err != nil {
		t.Fatalf("Failed to sign tx: %v", err)
	}

	if !Verify(unsigned, signedRaw.Sig, faucetPublicKey) {
		t.Fatalf("Self verify failed")
	}

	signTx := ToProtoSigTx(&signedRaw)
	res, err := client.AddTx(ctx, signTx)
	if err != nil {
		t.Fatalf("Failed to add tx: %v", err)
	}

	t.Logf("Transaction successful! Hash: %s", res.TxHash)

	time.Sleep(5 * time.Second)
	toAccount, err := client.GetAccount(ctx, toAddress)
	if err != nil {
		t.Fatalf("Failed to get account balance: %v", err)
	}

	t.Logf("Account %s balance: %s tokens, nonce: %d", toAddress, toAccount.Balance, toAccount.Nonce)
}

func TestClient_GetListTransactionsFaucet(t *testing.T) {
	client, err := defaultClient()
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()

	page := 1
	limit := 10
	offset := (page - 1) * limit
	filter := 0
	history, err := client.GetTxHistory(ctx, faucetAddress, limit, offset, filter)
	if err != nil {
		t.Fatalf("GetTxHistory failed: %v", err)
	}

	for _, tx := range history.Txs {
		t.Logf("Transaction Sender: %v, Recipient: %v, Amount: %v, Nonce: %v, Timestamp: %v, Status: %v", tx.Sender, tx.Recipient, tx.Amount, tx.Nonce, tx.Timestamp, tx.Status)
	}
}
