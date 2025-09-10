package client

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/holiman/uint256"
	mmnpb "github.com/mezonai/mmn/proto"
	"github.com/mr-tron/base58"
	"google.golang.org/grpc"
)

// Mirror Node test parameters
const (
	benchmarkUsers             = 20
	transactionsPerUser        = 15
	fundingAmountPerUser int64 = 30
)

// Faucet seed (last 32 bytes of PKCS#8 DER from node test)
var faucetSeedHex = "8e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee"

type goUser struct {
	publicKeyBase58 string
	seed            []byte
}

func makeClient(endpoint string) (*MmnClient, error) {
	return NewClient(Config{Endpoint: endpoint})
}

func getCurrentNonce(ctx context.Context, conn *grpc.ClientConn, address string) (uint64, error) {
	acc := mmnpb.NewAccountServiceClient(conn)
	resp, err := acc.GetCurrentNonce(ctx, &mmnpb.GetCurrentNonceRequest{Address: address, Tag: "pending"})
	if err != nil {
		return 0, err
	}
	return resp.Nonce, nil
}

func deriveAddressFromSeed(seed []byte) string {
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	return base58.Encode(pub)
}

func fundAccountWithRetry(ctx context.Context, c *MmnClient, faucetSeed []byte, recipient string, amount int64, note string, maxAttempts int) error {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		currentNonce, err := getCurrentNonce(ctx, c.Conn(), deriveAddressFromSeed(faucetSeed))
		if err != nil {
			if attempt == maxAttempts {
				return fmt.Errorf("get nonce failed: %w", err)
			}
			time.Sleep(time.Duration(200+attempt*100) * time.Millisecond)
			continue
		}
		nextNonce := currentNonce + 1

		amountU := uint256.NewInt(uint64(amount))
		fromAddr := deriveAddressFromSeed(faucetSeed)
		tx, err := BuildTransferTx(TxTypeTransfer, fromAddr, recipient, amountU, nextNonce, 0, note, nil)
		if err != nil {
			return err
		}
		sigTx, err := SignTx(tx, faucetSeed)
		if err != nil {
			return err
		}
		_, err = c.AddTx(ctx, sigTx)
		if err == nil {
			// small wait for processing
			time.Sleep(800 * time.Millisecond)
			return nil
		}
		// retry only on nonce-related errors
		if attempt < maxAttempts && (contains(err.Error(), "nonce") || contains(err.Error(), "invalid nonce")) {
			delay := time.Duration(400+randBetween(0, 800)) * time.Millisecond
			time.Sleep(delay)
			continue
		}
		if attempt == maxAttempts {
			return err
		}
	}
	return fmt.Errorf("unreachable")
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (stringIndex(s, sub) >= 0) }

// Minimal substring search to avoid extra deps
func stringIndex(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func randBetween(min, max int) int { // naive deterministic jitter
	return (int(time.Now().UnixNano()/1e6) % (max - min + 1)) + min
}

func TestRealisticTPS_Go(t *testing.T) {
	ctx := context.Background()
	// Create 3 clients (round-robin), like Node test
	endpoints := []string{"localhost:9001", "localhost:9002", "localhost:9003"}
	clients := make([]*MmnClient, 0, len(endpoints))
	for _, ep := range endpoints {
		c, err := makeClient(ep)
		if err != nil {
			t.Fatalf("create client %s: %v", ep, err)
		}
		clients = append(clients, c)
	}
	defer func() {
		for _, c := range clients {
			_ = c.Close()
		}
	}()

	seedBytes, _ := hex.DecodeString(faucetSeedHex)
	faucetAddr := deriveAddressFromSeed(seedBytes)
	t.Logf("Connected. Faucet: %s", faucetAddr)

	// Generate users
	users := make([]goUser, benchmarkUsers)
	for i := 0; i < benchmarkUsers; i++ {
		seed := make([]byte, 32)
		base := []byte(fmt.Sprintf("go_user_seed_%02d_go_user_seed_%02d", i, i))
		copy(seed, base)
		users[i] = goUser{
			publicKeyBase58: deriveAddressFromSeed(seed),
			seed:            seed,
		}
	}

	// Fund users sequentially (avoid faucet nonce collisions)
	for i, u := range users {
		if err := fundAccountWithRetry(ctx, clients[i%len(clients)], seedBytes, u.publicKeyBase58, fundingAmountPerUser, fmt.Sprintf("fund user %d", i), 5); err != nil {
			t.Fatalf("fund user %d failed: %v", i, err)
		}
	}

	// Match Node: wait ~3s for funding to be processed
	time.Sleep(3 * time.Second)

	// Prepare recipients (random selection)
	recipients := make([]goUser, 20)
	for i := 0; i < 20; i++ {
		seed := make([]byte, 32)
		base := []byte(fmt.Sprintf("go_recipient_%02d_seed", i))
		copy(seed, base)
		recipients[i] = goUser{publicKeyBase58: deriveAddressFromSeed(seed), seed: seed}
	}

	// Prepare transfers: each user sends transactionsPerUser txs
	var wg sync.WaitGroup
	var sendSuccess int64
	var sendFailure int64
	start := time.Now()

	// Track tx hashes and confirmation
	type txRecord struct {
		hash        string
		sentAt      time.Time
		confirmed   bool
		confirmedAt time.Time
	}
	totalTx := benchmarkUsers * transactionsPerUser
	records := make([]*txRecord, 0, totalTx)
	recMu := &sync.Mutex{}

	// Fetch base nonce per user from its assigned client to align with chain
	perUserBaseNonce := make(map[string]uint64)
	for ui, u := range users {
		c := clients[ui%len(clients)]
		base, err := getCurrentNonce(ctx, c.Conn(), u.publicKeyBase58)
		if err != nil {
			base = 0
		}
		perUserBaseNonce[u.publicKeyBase58] = base
	}

	sendTx := func(from goUser, userIdx int, txIdx int) {
		defer wg.Done()

		c := clients[userIdx%len(clients)]
		to := recipients[rand.Intn(len(recipients))]

		for attempt := 1; attempt <= 3; attempt++ {
			nextNonce := perUserBaseNonce[from.publicKeyBase58] + uint64(txIdx+1)
			amount := uint256.NewInt(1)
			tx, err := BuildTransferTx(TxTypeTransfer, from.publicKeyBase58, to.publicKeyBase58, amount, nextNonce, 0, fmt.Sprintf("go tps %d", userIdx*transactionsPerUser+txIdx), nil)
			if err != nil {
				atomic.AddInt64(&sendFailure, 1)
				return
			}
			sigTx, err := SignTx(tx, from.seed)
			if err != nil {
				atomic.AddInt64(&sendFailure, 1)
				return
			}
			res, err := c.AddTx(ctx, sigTx)
			if err != nil {
				// on nonce error, refresh base and retry
				if contains(err.Error(), "nonce") || contains(err.Error(), "invalid nonce") {
					base, gerr := getCurrentNonce(ctx, c.Conn(), from.publicKeyBase58)
					if gerr == nil {
						perUserBaseNonce[from.publicKeyBase58] = base
					}
					time.Sleep(time.Duration(100+rand.Intn(400)) * time.Millisecond)
					continue
				}
				atomic.AddInt64(&sendFailure, 1)
				return
			}
			recMu.Lock()
			records = append(records, &txRecord{hash: res.TxHash, sentAt: time.Now(), confirmed: false})
			recMu.Unlock()
			atomic.AddInt64(&sendSuccess, 1)
			return
		}
		atomic.AddInt64(&sendFailure, 1)
	}

	for ui := 0; ui < benchmarkUsers; ui++ {
		from := users[ui]
		for j := 0; j < transactionsPerUser; j++ {
			wg.Add(1)
			go sendTx(from, ui, j)
		}
	}

	wg.Wait()

	// Wait for confirmations (end-to-end). Poll until all recorded txs are confirmed or timeout.
	deadline := time.Now().Add(60 * time.Second)
	confirmedCount := 0
	for {
		if time.Now().After(deadline) {
			break
		}
		recMu.Lock()
		remaining := 0
		for _, r := range records {
			if r.confirmed {
				continue
			}
			remaining++
			info, err := clients[0].GetTxByHash(ctx, r.hash)
			if err == nil && (info.Status == int32(TxMeta_Status_CONFIRMED) || info.Status == int32(TxMeta_Status_FINALIZED)) {
				r.confirmed = true
				r.confirmedAt = time.Now()
			}
		}
		recMu.Unlock()
		if remaining == 0 {
			break
		}
	}

	// Compute end-to-end window: from min(sentAt) to max(confirmedAt)
	var minSent time.Time
	var maxConfirmed time.Time
	confirmedCount = 0
	recMu.Lock()
	for _, r := range records {
		if minSent.IsZero() || r.sentAt.Before(minSent) {
			minSent = r.sentAt
		}
		if r.confirmed {
			confirmedCount++
			if r.confirmedAt.After(maxConfirmed) {
				maxConfirmed = r.confirmedAt
			}
		}
	}
	recMu.Unlock()

	window := maxConfirmed.Sub(minSent)
	if window <= 0 {
		window = time.Since(start)
	}
	TPS := float64(confirmedCount) / window.Seconds()

	fmt.Println("=====================================")
	fmt.Printf("Users: %d\n", benchmarkUsers)
	fmt.Printf("Transactions per User: %d\n", transactionsPerUser)
	fmt.Printf("Total Transactions: %d\n", benchmarkUsers*transactionsPerUser)
	fmt.Printf("Sent Successful: %d\n", sendSuccess)
	fmt.Printf("Sent Failed: %d\n", sendFailure)
	fmt.Printf("Confirmed: %d\n", confirmedCount)
	fmt.Printf("Window Time (send->last confirm): %.2fs\n", window.Seconds())
	fmt.Printf("End-to-End TPS: %.2f\n", TPS)
	fmt.Println("=====================================")

	if confirmedCount == 0 {
		t.Fatalf("no successful transactions")
	}
}
