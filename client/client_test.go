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
	toAddress := "8BH3ZXoAptWYbAc69221kKDrrPzvc4RaJ248qdbTs6k5" // dummy base58 for test

	// Get current faucet account to get the next nonce
	faucetAccount, err := client.GetAccount(ctx, faucetPublicKey)
	if err != nil {
		t.Fatalf("Failed to get faucet account: %v", err)
	}
	nextNonce := faucetAccount.Nonce + 1
	t.Logf("Faucet account nonce: %d, using next nonce: %d", faucetAccount.Nonce, nextNonce)

	// Extract the seed from the private key (first 32 bytes)
	faucetPrivateKeySeed := faucetPrivateKey.Seed()
	transferType := TxTypeFaucet
	fromAddr := faucetPublicKey
	fromAccount, err := client.GetAccount(ctx, fromAddr)
	if err != nil {
		t.Fatalf("Failed to get account: %v", err)
	}

	toAddr := toAddress
	amount := uint256.NewInt(10000000000000)
	nonce := fromAccount.Nonce + 1
	textData := "Integration test transfer"

	extraInfo := map[string]string{
		"type": "unlock_item",
	}

	unsigned, err := BuildTransferTx(transferType, fromAddr, toAddr, amount, nonce, uint64(time.Now().Unix()), textData, extraInfo, "", "")
	if err != nil {
		t.Fatalf("Failed to build transfer tx: %v", err)
	}

	signedRaw, err := SignTx(unsigned, []byte(faucetPublicKey), faucetPrivateKeySeed)
	if err != nil {
		t.Fatalf("Failed to sign tx: %v", err)
	}

	if !Verify(unsigned, signedRaw.Sig) {
		t.Fatalf("Self verify failed")
	}

	res, err := client.AddTx(ctx, signedRaw)
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

	// verify tx extra
	actualTxInfo, err := client.GetTxByHash(ctx, res.TxHash)
	if err != nil {
		t.Fatalf("Failed to get tx by hash: %v", err)
	}
	t.Logf("Transaction info: %+v", actualTxInfo)
	actualTxExtra, err := actualTxInfo.DeserializedExtraInfo()
	if err != nil {
		t.Fatalf("Failed to deserialize tx extra info: %v", err)
	}
	if actualTxExtra == nil || actualTxExtra["type"] != "unlock_item" {
		t.Errorf("Unmatched tx extra info: expected: %+v, actual: %+v", extraInfo, actualTxExtra)
	}
}

func TestClient_SendToken(t *testing.T) {
	client, err := defaultClient()
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()

	fromAddress := "8BH3ZXoAptWYbAc69221kKDrrPzvc4RaJ248qdbTs6k5" // dummy base58 for test
	fromPrivateKeyHex := "302e020100300506032b657004220420f6d1c48c25d3705715959eb1a750cafe8ce1a95ef9738a4d634b24f206dad506"
	fromPublicKeyHex := "2Bq5iv3hxDf7Z8moNVmLzKKKFBWoV48BZ1M1ppqqRJ5j"
	toAddress := "CanBzWYv7Rf21DYZR5oDoon7NJmhLQ32eUvmyDGkeyK7" // dummy base58 for test

	fromPrivateKeyDer, err := hex.DecodeString(fromPrivateKeyHex)
	if err != nil {
		t.Fatalf("Failed to decode from private key: %v", err)
	}
	fromSeed := fromPrivateKeyDer[len(fromPrivateKeyDer)-32:]
	fromPrivateKey := ed25519.NewKeyFromSeed(fromSeed)

	fromAccount, err := client.GetAccount(ctx, fromAddress)
	if err != nil {
		t.Fatalf("Failed to get account: %v", err)
	}
	nextNonce := fromAccount.Nonce + 1
	t.Logf("From account nonce: %d, using next nonce: %d", fromAccount.Nonce, nextNonce)

	amount := uint256.NewInt(1)
	nonce := fromAccount.Nonce + 1
	textData := "Integration test transfer"

	extraInfo := map[string]string{
		"type": "transfer",
	}

	zkProof := "pqbM64ZFEgCkTEY9hB0DoeBPjVypF94nN67JOm5HPvSV3yRXHoVMJlHR+li9ZzPTEuLpG3KjWFjtFoVQVdg3zgVWRU7WSk7ogRAVQ6SWfV9dkyH16KPRYPqDDTbe13mmpugxjHp8J5gXQMPLHBajbWS8r8ifXWDTYLPb6ubFJiAAAAAAQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	zkPub := "AAAABAAAAAAAAAAEGXCTN8ZNN3H071Ika7f1l+1tIUWLtsQ5FaXqv9c2L7cAAAAAAAAAAAAAAAAAMzc2NzQ3ODQzMjE2MzE3Mjk5OSSG388WlTRfAZ3r3w6bu8EmVbNlf9dyuS25NeJwx9gmKur1plPc1AnRoYtefb3mzUMHLgWArBdW8RKHMQ8NeV4="
	unsigned, err := BuildTransferTx(TxTypeTransfer, fromAddress, toAddress, amount, nonce, uint64(time.Now().Unix()), textData, extraInfo, zkProof, zkPub)
	if err != nil {
		t.Fatalf("Failed to build transfer tx: %v", err)
	}

	fromPublicKey, err := base58.Decode(fromPublicKeyHex)
	if err != nil {
		t.Fatalf("Failed to decode from public key: %v", err)
	}
	signedRaw, err := SignTx(unsigned, fromPublicKey, fromPrivateKey.Seed())
	if err != nil {
		t.Fatalf("Failed to sign tx: %v", err)
	}

	if !Verify(unsigned, signedRaw.Sig) {
		t.Fatalf("Self verify failed")
	}

	res, err := client.AddTx(ctx, signedRaw)
	if err != nil {
		t.Fatalf("Failed to add tx: %v", err)
	}

	t.Logf("Transaction successful! Hash: %s", res.TxHash)

	time.Sleep(5 * time.Second)
	fromAccount, err = client.GetAccount(ctx, fromAddress)
	if err != nil {
		t.Fatalf("Failed to get account balance: %v", err)
	}

	t.Logf("Account %s balance: %s tokens, nonce: %d", fromAddress, fromAccount.Balance, fromAccount.Nonce)

	toAccount, err := client.GetAccount(ctx, toAddress)
	if err != nil {
		t.Fatalf("Failed to get account: %v", err)
	}

	t.Logf("Account %s balance: %s tokens, nonce: %d", toAddress, toAccount.Balance, toAccount.Nonce)
}
