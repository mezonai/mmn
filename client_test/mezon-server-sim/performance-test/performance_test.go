package performance_test

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/holiman/uint256"
	mmnclient "github.com/mezonai/mmn/client"
	mmnpb "github.com/mezonai/mmn/proto"
	"github.com/mr-tron/base58"
	"google.golang.org/grpc"
)

const faucetSeedHex = "8e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee"

func TestPerformance_SendToken(t *testing.T) {
	ctx := context.Background()

	endpoints := []string{"localhost:9001", "localhost:9002", "localhost:9003"}
	clients := make([]*mmnclient.MmnClient, 0, len(endpoints))
	for _, ep := range endpoints {
		c, err := mmnclient.NewClient(mmnclient.Config{Endpoint: ep})
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

	// Lower burst by using more users and fewer tx per user; allow override via env
	usersCount := 120
	txPerUser := 20

	type goUser struct {
		publicKeyBase58 string
		seed            []byte
	}
	derive := func(seed []byte) string {
		pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
		return base58.Encode(pub)
	}

	users := make([]goUser, usersCount)
	for i := 0; i < usersCount; i++ {
		seed := make([]byte, 32)
		copy(seed, []byte(fmt.Sprintf("test_user_%02d_seed________________", i)))
		users[i] = goUser{publicKeyBase58: derive(seed), seed: seed}
	}
	recipients := make([]goUser, usersCount)
	for i := 0; i < usersCount; i++ {
		seed := make([]byte, 32)
		copy(seed, []byte(fmt.Sprintf("test_rcp_%02d_seed_________________", i)))
		recipients[i] = goUser{publicKeyBase58: derive(seed), seed: seed}
	}

	// fund users sequentially with batch confirm to avoid faucet pending overflow
	batchSize := 20
	batchTimeoutSec := 20
	batch := make([]string, 0, batchSize)
	for i, u := range users {
		h, err := fundAccountWithRetry(ctx, clients[i%len(clients)], seedBytes, u.publicKeyBase58, 100, fmt.Sprintf("test fund %d", i), 5)
		if err != nil {
			t.Fatalf("fund user %d failed: %v", i, err)
		}
		batch = append(batch, h)
		if len(batch) == batchSize {
			waitBatchFundConfirmed(ctx, clients[i%len(clients)], batch, time.Duration(batchTimeoutSec)*time.Second)
			batch = batch[:0]
		}
		// light pacing
		time.Sleep(2 * time.Millisecond)
	}
	if len(batch) > 0 {
		waitBatchFundConfirmed(ctx, clients[(len(users)-1)%len(clients)], batch, time.Duration(batchTimeoutSec)*time.Second)
	}

	stream, err := clients[0].SubscribeTransactionStatus(ctx)
	if err != nil {
		t.Fatalf("subscribe transaction status: %v", err)
	}
	// short warm-up to ensure stream readiness without affecting measurement window
	time.Sleep(200 * time.Millisecond)

	type stageTimes struct {
		sentAt      time.Time
		ackAt       time.Time
		pendingAt   time.Time
		confirmedAt time.Time
		finalizedAt time.Time
	}
	txTimes := make(map[string]*stageTimes, usersCount*txPerUser)
	mu := &sync.Mutex{}

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		for {
			msg, recvErr := stream.Recv()
			if recvErr != nil {
				return
			}
			if msg == nil || msg.TxHash == "" {
				continue
			}
			mu.Lock()
			rec := txTimes[msg.TxHash]
			if rec == nil {
				rec = &stageTimes{}
				txTimes[msg.TxHash] = rec
			}
			if msg.Status == mmnpb.TransactionStatus_PENDING {
				if rec.pendingAt.IsZero() {
					rec.pendingAt = time.Now()
				}
			}
			if msg.Status == mmnpb.TransactionStatus_CONFIRMED {
				if rec.confirmedAt.IsZero() {
					rec.confirmedAt = time.Now()
				}
			}
			if msg.Status == mmnpb.TransactionStatus_FINALIZED {
				if rec.finalizedAt.IsZero() {
					rec.finalizedAt = time.Now()
				}
			}
			mu.Unlock()
		}
	}()

	perUserBaseNonce := make(map[string]uint64)
	for ui, u := range users {
		c := clients[ui%len(clients)]
		base, gerr := getCurrentNonce(ctx, c.Conn(), u.publicKeyBase58)
		if gerr != nil {
			base = 0
		}
		perUserBaseNonce[u.publicKeyBase58] = base
	}

	var sentOk int64
	var sentFail int64
	var wg sync.WaitGroup

	for ui := 0; ui < usersCount; ui++ {
		from := users[ui]
		for j := 0; j < txPerUser; j++ {
			wg.Add(1)
			go func(userIdx, txIdx int, from goUser) {
				defer wg.Done()
				c := clients[userIdx%len(clients)]
				to := recipients[rand.Intn(len(recipients))]
				for attempt := 1; attempt <= 3; attempt++ {
					nextNonce := perUserBaseNonce[from.publicKeyBase58] + uint64(txIdx+1)
					amount := uint256.NewInt(1)
					tx, berr := mmnclient.BuildTransferTx(mmnclient.TxTypeTransfer, from.publicKeyBase58, to.publicKeyBase58, amount, nextNonce, 0, fmt.Sprintf("test_%d_%d", userIdx, txIdx), nil)
					if berr != nil {
						atomic.AddInt64(&sentFail, 1)
						return
					}
					sigTx, serr := mmnclient.SignTx(tx, from.seed)
					if serr != nil {
						atomic.AddInt64(&sentFail, 1)
						return
					}

					sentAt := time.Now()
					res, aerr := c.AddTx(ctx, sigTx)
					if aerr != nil {
						if contains(aerr.Error(), "nonce") || contains(aerr.Error(), "invalid nonce") {
							base, gerr := getCurrentNonce(ctx, c.Conn(), from.publicKeyBase58)
							if gerr == nil {
								perUserBaseNonce[from.publicKeyBase58] = base
							}
							time.Sleep(2 * time.Millisecond)
							continue
						}
						atomic.AddInt64(&sentFail, 1)
						return
					}
					ackAt := time.Now()
					mu.Lock()
					st := txTimes[res.TxHash]
					if st == nil {
						st = &stageTimes{}
						txTimes[res.TxHash] = st
					}
					if st.sentAt.IsZero() {
						st.sentAt = sentAt
					}
					st.ackAt = ackAt
					mu.Unlock()

					atomic.AddInt64(&sentOk, 1)
					return
				}
				atomic.AddInt64(&sentFail, 1)
			}(ui, j, from)
		}
	}

	start := time.Now()
	wg.Wait()

	timeout := time.After(300 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			goto compute
		case <-ticker.C:
			mu.Lock()
			confirmed := 0
			finalized := 0
			for _, st := range txTimes {
				if !st.confirmedAt.IsZero() {
					confirmed++
				}
				if !st.finalizedAt.IsZero() {
					finalized++
				}
			}
			mu.Unlock()
			if int(sentOk) > 0 && confirmed >= int(sentOk) && finalized >= int(sentOk) {
				goto compute
			}
		case <-doneCh:
			goto compute
		}
	}

compute:
	mu.Lock()
	var firstSent, lastPending, lastConfirmed, lastFinalized time.Time
	ingressCount := 0
	executedCount := 0
	finalizedCount := 0
	for _, st := range txTimes {
		if st.sentAt.IsZero() {
			continue
		}
		if firstSent.IsZero() || st.sentAt.Before(firstSent) {
			firstSent = st.sentAt
		}
		if !st.pendingAt.IsZero() {
			ingressCount++
			if st.pendingAt.After(lastPending) {
				lastPending = st.pendingAt
			}
		}
		if !st.confirmedAt.IsZero() {
			executedCount++
			if st.confirmedAt.After(lastConfirmed) {
				lastConfirmed = st.confirmedAt
			}
		}
		if !st.finalizedAt.IsZero() {
			finalizedCount++
			if st.finalizedAt.After(lastFinalized) {
				lastFinalized = st.finalizedAt
			}
		}
	}
	mu.Unlock()

	if firstSent.IsZero() {
		firstSent = start
	}
	ingressWindow := lastPending.Sub(firstSent)
	executedWindow := lastConfirmed.Sub(firstSent)
	finalizedWindow := lastFinalized.Sub(firstSent)
	if ingressWindow <= 0 {
		ingressWindow = time.Since(start)
	}
	if executedWindow <= 0 {
		executedWindow = time.Since(start)
	}
	if finalizedWindow <= 0 {
		finalizedWindow = time.Since(start)
	}

	ingressTPS := float64(ingressCount) / ingressWindow.Seconds()
	executedTPS := float64(executedCount) / executedWindow.Seconds()
	finalizedTPS := float64(finalizedCount) / finalizedWindow.Seconds()

	// Calculate efficiency metrics
	ingressToExecutedRatio := float64(executedCount) / float64(ingressCount) * 100
	executedToFinalizedRatio := float64(finalizedCount) / float64(executedCount) * 100
	overallEfficiency := float64(finalizedCount) / float64(ingressCount) * 100

	// Calculate bottleneck analysis
	ingressBottleneck := ingressTPS / executedTPS
	executionBottleneck := executedTPS / finalizedTPS

	t.Logf("=== PERFORMANCE RESULTS ===")
	t.Logf("Sent ok=%d fail=%d", sentOk, sentFail)
	t.Logf("")
	t.Logf("=== TPS METRICS ===")
	t.Logf("Ingress TPS:  %.2f (count=%d, window=%.2fs)", ingressTPS, ingressCount, ingressWindow.Seconds())
	t.Logf("Executed TPS: %.2f (count=%d, window=%.2fs)", executedTPS, executedCount, executedWindow.Seconds())
	t.Logf("Finalized TPS: %.2f (count=%d, window=%.2fs)", finalizedTPS, finalizedCount, finalizedWindow.Seconds())
	t.Logf("")

	// Performance assessment
	t.Logf("")
	t.Logf("=== PERFORMANCE ASSESSMENT ===")
	if ingressTPS > 1000 {
		t.Logf("✅ Ingress TPS: EXCELLENT (>1000 TPS)")
	} else if ingressTPS > 500 {
		t.Logf("🟡 Ingress TPS: GOOD (500-1000 TPS)")
	} else {
		t.Logf("❌ Ingress TPS: NEEDS IMPROVEMENT (<500 TPS)")
	}

	if executedTPS > 500 {
		t.Logf("✅ Executed TPS: EXCELLENT (>500 TPS)")
	} else if executedTPS > 100 {
		t.Logf("🟡 Executed TPS: GOOD (100-500 TPS)")
	} else {
		t.Logf("❌ Executed TPS: NEEDS IMPROVEMENT (<100 TPS)")
	}

	if overallEfficiency > 90 {
		t.Logf("✅ Overall Efficiency: EXCELLENT (>90%%)")
	} else if overallEfficiency > 70 {
		t.Logf("🟡 Overall Efficiency: GOOD (70-90%%)")
	} else {
		t.Logf("❌ Overall Efficiency: NEEDS IMPROVEMENT (<70%%)")
	}

	// Write summarized results to JSON file
	results := map[string]any{
		"timestamp":                time.Now().Format(time.RFC3339Nano),
		"usersCount":               usersCount,
		"txPerUser":                txPerUser,
		"sentOk":                   sentOk,
		"sentFail":                 sentFail,
		"ingressWindowSec":         ingressWindow.Seconds(),
		"executedWindowSec":        executedWindow.Seconds(),
		"finalizedWindowSec":       finalizedWindow.Seconds(),
		"ingressCount":             ingressCount,
		"executedCount":            executedCount,
		"finalizedCount":           finalizedCount,
		"ingressTPS":               ingressTPS,
		"executedTPS":              executedTPS,
		"finalizedTPS":             finalizedTPS,
		"ingressToExecutedRatio":   ingressToExecutedRatio,
		"executedToFinalizedRatio": executedToFinalizedRatio,
		"overallEfficiency":        overallEfficiency,
		"ingressBottleneck":        ingressBottleneck,
		"executionBottleneck":      executionBottleneck,
		"performanceAssessment": map[string]string{
			"ingressTPS":  getPerformanceLevel(ingressTPS, 1000, 500),
			"executedTPS": getPerformanceLevel(executedTPS, 500, 100),
			"efficiency":  getEfficiencyLevel(overallEfficiency),
		},
	}
	if b, err := json.MarshalIndent(results, "", "  "); err == nil {
		_ = os.WriteFile("performance_results.json", b, 0644)
	} else {
		t.Logf("failed to marshal results json: %v", err)
	}

	if sentOk == 0 {
		t.Fatalf("no transactions sent successfully")
	}
}

func deriveAddressFromSeed(seed []byte) string {
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	return base58.Encode(pub)
}

func fundAccountWithRetry(ctx context.Context, c *mmnclient.MmnClient, faucetSeed []byte, recipient string, amount int64, note string, maxAttempts int) (string, error) {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		currentNonce, err := getCurrentNonce(ctx, c.Conn(), deriveAddressFromSeed(faucetSeed))
		if err != nil {
			if attempt == maxAttempts {
				return "", fmt.Errorf("get nonce failed: %w", err)
			}
			time.Sleep(time.Duration(50+attempt*20) * time.Millisecond)
			continue
		}
		nextNonce := currentNonce + 1
		amountU := uint256.NewInt(uint64(amount))
		fromAddr := deriveAddressFromSeed(faucetSeed)
		tx, err := mmnclient.BuildTransferTx(mmnclient.TxTypeTransfer, fromAddr, recipient, amountU, nextNonce, 0, note, nil)
		if err != nil {
			return "", err
		}
		sigTx, err := mmnclient.SignTx(tx, faucetSeed)
		if err != nil {
			return "", err
		}
		res, err := c.AddTx(ctx, sigTx)
		if err == nil {
			return res.TxHash, nil
		}
		if attempt < maxAttempts && (contains(err.Error(), "nonce") || contains(err.Error(), "invalid nonce")) {
			time.Sleep(time.Duration(40+randBetween(0, 200)) * time.Millisecond)
			continue
		}
		if attempt == maxAttempts {
			return "", err
		}
	}
	return "", fmt.Errorf("unreachable")
}

func waitBatchFundConfirmed(ctx context.Context, client *mmnclient.MmnClient, hashes []string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	confirmed := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		confirmed[h] = false
	}
	for {
		if time.Now().After(deadline) {
			return
		}
		pending := 0
		for h := range confirmed {
			if confirmed[h] {
				continue
			}
			info, err := client.GetTxByHash(ctx, h)
			if err == nil && (info.Status == 1 || info.Status == 2) { // CONFIRMED or FINALIZED
				confirmed[h] = true
				continue
			}
			pending++
		}
		if pending == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func getCurrentNonce(ctx context.Context, conn *grpc.ClientConn, address string) (uint64, error) {
	acc := mmnpb.NewAccountServiceClient(conn)
	resp, err := acc.GetCurrentNonce(ctx, &mmnpb.GetCurrentNonceRequest{Address: address, Tag: "pending"})
	if err != nil {
		return 0, err
	}
	return resp.Nonce, nil
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (stringIndex(s, sub) >= 0)
}

func stringIndex(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func randBetween(min, max int) int {
	return (int(time.Now().UnixNano()/1e6) % (max - min + 1)) + min
}

// Helper functions for performance assessment
func getPerformanceLevel(tps float64, excellentThreshold, goodThreshold float64) string {
	if tps > excellentThreshold {
		return "EXCELLENT"
	} else if tps > goodThreshold {
		return "GOOD"
	}
	return "NEEDS_IMPROVEMENT"
}

func getEfficiencyLevel(efficiency float64) string {
	if efficiency > 90 {
		return "EXCELLENT"
	} else if efficiency > 70 {
		return "GOOD"
	}
	return "NEEDS_IMPROVEMENT"
}

// CalculateTPSSummary calculates and returns a summary of TPS metrics
func CalculateTPSSummary(ingressCount, executedCount, finalizedCount int,
	ingressWindow, executedWindow, finalizedWindow time.Duration) map[string]interface{} {

	ingressTPS := float64(ingressCount) / ingressWindow.Seconds()
	executedTPS := float64(executedCount) / executedWindow.Seconds()
	finalizedTPS := float64(finalizedCount) / finalizedWindow.Seconds()

	// Calculate efficiency metrics
	ingressToExecutedRatio := float64(executedCount) / float64(ingressCount) * 100
	executedToFinalizedRatio := float64(finalizedCount) / float64(executedCount) * 100
	overallEfficiency := float64(finalizedCount) / float64(ingressCount) * 100

	// Calculate bottleneck analysis
	ingressBottleneck := ingressTPS / executedTPS
	executionBottleneck := executedTPS / finalizedTPS

	return map[string]interface{}{
		"tps": map[string]float64{
			"ingress":   ingressTPS,
			"executed":  executedTPS,
			"finalized": finalizedTPS,
		},
		"counts": map[string]int{
			"ingress":   ingressCount,
			"executed":  executedCount,
			"finalized": finalizedCount,
		},
		"windows": map[string]float64{
			"ingress":   ingressWindow.Seconds(),
			"executed":  executedWindow.Seconds(),
			"finalized": finalizedWindow.Seconds(),
		},
		"efficiency": map[string]float64{
			"ingress_to_executed":   ingressToExecutedRatio,
			"executed_to_finalized": executedToFinalizedRatio,
			"overall":               overallEfficiency,
		},
		"bottlenecks": map[string]float64{
			"ingress_ratio":   ingressBottleneck,
			"execution_ratio": executionBottleneck,
		},
		"assessment": map[string]string{
			"ingress_tps":  getPerformanceLevel(ingressTPS, 1000, 500),
			"executed_tps": getPerformanceLevel(executedTPS, 500, 100),
			"efficiency":   getEfficiencyLevel(overallEfficiency),
		},
	}
}
