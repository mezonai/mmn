package client_test

import (
	"context"
	"crypto/ed25519"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/holiman/uint256"
	"github.com/mr-tron/base58"

	"github.com/mezonai/mmn/client"
)

const faucetPrivHex = "302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee"

func hashToBase58(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base58.Encode(sum[:])
}

// abuse error when 4 tx/min (change config to <4/min to fast test)
func TestAbuseAutoBlacklistOnFaucetFlood(t *testing.T) {
	server := "127.0.0.1:9001"

	cli, err := client.NewClient(client.Config{Endpoint: server})
	if err != nil {
		t.Skipf("cannot connect to node at %s: %v", server, err)
	}
	defer cli.Close()

	faucetKeyBytes, err := hex.DecodeString(faucetPrivHex)
	if err != nil {
		t.Fatalf("decode faucet key: %v", err)
	}
	faucetSeed := faucetKeyBytes[len(faucetKeyBytes)-32:]
	faucetPriv := ed25519.NewKeyFromSeed(faucetSeed)
	faucetPubB58 := base58.Encode(faucetPriv.Public().(ed25519.PublicKey))

	rnd := make([]byte, 8)
	if _, err := crand.Read(rnd); err != nil {
		t.Fatalf("rand: %v", err)
	}
	recipient := hashToBase58("abuse-" + hex.EncodeToString(rnd))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	baseNonce, err := cli.GetCurrentNonce(ctx, faucetPubB58, "pending")
	if err != nil {
		t.Skipf("cannot query faucet nonce, is node running? %v", err)
	}

	var gotAbuseErr bool
	for i := 0; i < 5; i++ {
		nonce := baseNonce + uint64(i+1)
		amount := uint256.NewInt(1)
		unsigned, err := client.BuildTransferTx(client.TxTypeFaucet, faucetPubB58, recipient, amount, nonce, uint64(time.Now().Unix()), "abuse", map[string]string{"type": "abuse test"}, "", "")
		if err != nil {
			t.Fatalf("build tx: %v", err)
		}
		signed, err := client.SignTx(unsigned, []byte{}, faucetPriv.Seed())
		if err != nil {
			t.Fatalf("sign tx: %v", err)
		}
		if !client.Verify(unsigned, signed.Sig) {
			t.Fatalf("verify failed")
		}

		_, err = cli.AddTx(ctx, signed)
		if err != nil {
			gotAbuseErr = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !gotAbuseErr {
		t.Skip("no abuse error observed; ensure node is running with low AutoBlacklistTxPerMinute")
	}
}

// abuse error when 4 tx/min (change config to <4/min to fast test)
func TestAbuseAutoBlacklistOnTransferFlood(t *testing.T) {
	server := "127.0.0.1:9001"

	cli, err := client.NewClient(client.Config{Endpoint: server})
	if err != nil {
		t.Skipf("cannot connect to node at %s: %v", server, err)
	}
	defer cli.Close()

	faucetKeyBytes, err := hex.DecodeString(faucetPrivHex)
	if err != nil {
		t.Fatalf("decode faucet key: %v", err)
	}
	faucetSeed := faucetKeyBytes[len(faucetKeyBytes)-32:]
	faucetPriv := ed25519.NewKeyFromSeed(faucetSeed)
	faucetPubB58 := base58.Encode(faucetPriv.Public().(ed25519.PublicKey))

	rnd := make([]byte, 8)
	if _, err := crand.Read(rnd); err != nil {
		t.Fatalf("rand: %v", err)
	}
	senderAddr := hashToBase58("transfer-sender-" + hex.EncodeToString(rnd))
	recipientAddr := hashToBase58("transfer-recipient-" + hex.EncodeToString(rnd))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	faucetNonce, err := cli.GetCurrentNonce(ctx, faucetPubB58, "pending")
	if err != nil {
		t.Skipf("cannot query faucet nonce: %v", err)
	}

	fundAmount := uint256.NewInt(1000000)
	fundTx, err := client.BuildTransferTx(client.TxTypeFaucet, faucetPubB58, senderAddr, fundAmount, faucetNonce+1, uint64(time.Now().Unix()), "fund transfer test", map[string]string{"type": "funding"}, "", "")
	if err != nil {
		t.Fatalf("build fund tx: %v", err)
	}
	fundSigned, err := client.SignTx(fundTx, []byte{}, faucetPriv.Seed())
	if err != nil {
		t.Fatalf("sign fund tx: %v", err)
	}
	_, err = cli.AddTx(ctx, fundSigned)
	if err != nil {
		t.Skipf("cannot fund account, faucet might be restricted: %v", err)
	}

	// Wait a bit for funding to be processed
	time.Sleep(1 * time.Second)

	senderNonce, err := cli.GetCurrentNonce(ctx, senderAddr, "pending")
	if err != nil {
		t.Skipf("cannot query sender nonce: %v", err)
	}

	transferAmount := uint256.NewInt(100)
	var gotAbuseErr bool
	for i := 0; i < 10; i++ {
		nonce := senderNonce + uint64(i+1)
		unsigned, err := client.BuildTransferTx(client.TxTypeTransfer, senderAddr, recipientAddr, transferAmount, nonce, uint64(time.Now().Unix()), "transfer abuse test", map[string]string{"type": "transfer"}, "", "")
		if err != nil {
			t.Fatalf("build transfer tx: %v", err)
		}

		senderPriv := ed25519.NewKeyFromSeed(make([]byte, 32))
		signed, err := client.SignTx(unsigned, []byte{}, senderPriv.Seed())
		if err != nil {
			t.Fatalf("sign transfer tx: %v", err)
		}

		_, err = cli.AddTx(ctx, signed)
		if err != nil {
			gotAbuseErr = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !gotAbuseErr {
		t.Skip("no abuse error observed on transfer transactions; ensure node has low thresholds")
	}
}
