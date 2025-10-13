package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	jwt "github.com/golang-jwt/jwt/v4"
	"github.com/holiman/uint256"
	"github.com/mr-tron/base58"

	"github.com/mezonai/mmn/client"
)

// Configuration
type Config struct {
	ServerAddress  string
	AccountCount   int
	TxPerSecond    int
	SwitchAfterTx  int
	FundAmount     uint64
	TransferAmount uint64
	Duration       time.Duration
	RunMinutes     int // Run for specified minutes, then stop
	Workers        int // Number of concurrent send workers
}

// Account represents a test account
type Account struct {
	PublicKey  string
	PrivateKey ed25519.PrivateKey
	Nonce      uint64
	Balance    uint64
	Address    string
	ZkProof    string
	ZkPub      string
}

// LoadTester handles the load testing
type LoadTester struct {
	config   Config
	accounts []Account
	client   *client.MmnClient
	ctx      context.Context
	cancel   context.CancelFunc

	// Statistics
	totalTxsSent     int64
	totalTxsSuccess  int64
	totalTxsFailed   int64
	fundingStartTime time.Time
	testStartTime    time.Time

	// System metrics tracking
	metricsTicker *time.Ticker

	// Logger
	logger *Logger

	// Faucet account (hardcoded from genesis)
	faucetPrivateKey ed25519.PrivateKey
	faucetPublicKey  string

	// Per-account nonce tracking (align with performance test)
	perAccountBaseNonce   map[string]uint64
	perAccountLocalCounts map[string]uint64
	nonceMutex            sync.Mutex

	// Faucet nonce serialization to avoid duplicate nonce under parallel refills
	faucetNonce     uint64
	faucetNonceInit bool
	faucetNonceMu   sync.Mutex

	// Ensure only one faucet tx (fund/refill) is sent at a time
	faucetSendMu sync.Mutex
}

type SessionTokenClaims struct {
	TokenId   string            `json:"tid,omitempty"`
	UserId    int64             `json:"uid,omitempty"`
	Username  string            `json:"usn,omitempty"`
	Vars      map[string]string `json:"vrs,omitempty"`
	ExpiresAt int64             `json:"exp,omitempty"`
}

func (stc *SessionTokenClaims) Valid() error {
	// Verify expiry.
	if stc.ExpiresAt <= time.Now().UTC().Unix() {
		vErr := new(jwt.ValidationError)
		vErr.Inner = errors.New("token is expired")
		vErr.Errors |= jwt.ValidationErrorExpired
		return vErr
	}
	return nil
}

type ProveData struct {
	Proof       string `json:"proof"`
	PublicInput string `json:"public_input"`
}

type ProveResponse struct {
	Success bool      `json:"success"`
	Data    ProveData `json:"data"`
	Message string    `json:"message"`
}

// Faucet private key from genesis (same as in TypeScript tests)
const (
	FaucetPrivateKeyHex = "302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee"
	JwtSecret           = "defaultencryptionkey"
	ZkVerifyUrl         = "http://172.16.100.180:8282"
)

// Local copies of node-side limits to throttle faucet
const FaucetMaxFutureNonceWindow = 64
const FaucetMaxPendingPerSender = 60

// logRealTimeMetrics logs current system metrics and transaction stats
func (lt *LoadTester) logRealTimeMetrics() {
	// Get current transaction stats
	totalTxs := atomic.LoadInt64(&lt.totalTxsSent)
	successTxs := atomic.LoadInt64(&lt.totalTxsSuccess)
	failedTxs := atomic.LoadInt64(&lt.totalTxsFailed)

	// Use logger to write real-time metrics
	lt.logger.LogRealTimeMetrics(totalTxs, successTxs, failedTxs, lt.testStartTime, lt.config)
}

func main() {
	config := parseFlags()

	// Create load tester
	tester, err := NewLoadTester(config)
	if err != nil {
		log.Fatalf("Failed to create load tester: %v", err)
	}
	defer tester.Close()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start load testing in a goroutine
	go func() {
		if err := tester.Run(); err != nil {
			tester.logger.LogError("Load test error: %v", err)
		}
	}()

	// Wait for signal or completion
	select {
	case <-sigChan:
		tester.logger.LogInfo("Received shutdown signal, stopping load test...")
		tester.Stop()
	case <-tester.ctx.Done():
		tester.logger.LogInfo("Load test completed")
	}

	// Print final statistics
	tester.PrintStats()
}

func parseFlags() Config {
	var config Config

	flag.StringVar(&config.ServerAddress, "server", "127.0.0.1:9001", "gRPC server address")
	flag.IntVar(&config.AccountCount, "accounts", 100, "Number of accounts to create")
	flag.IntVar(&config.TxPerSecond, "rate", 50, "Transactions per second")
	flag.IntVar(&config.SwitchAfterTx, "switch", 10, "Switch account after N transactions")
	flag.IntVar(&config.Workers, "workers", 100, "Number of concurrent send workers")
	flag.Uint64Var(&config.FundAmount, "fund", 10000000000, "Amount to fund each account")
	flag.Uint64Var(&config.TransferAmount, "amount", 100, "Amount per transfer transaction")
	flag.DurationVar(&config.Duration, "duration", 0, "Test duration (0 = run indefinitely)")
	flag.IntVar(&config.RunMinutes, "minutes", 0, "Run for specified minutes, then stop (0 = run indefinitely)")

	flag.Parse()

	// If minutes is specified, convert to duration and override duration
	if config.RunMinutes > 0 {
		config.Duration = time.Duration(config.RunMinutes) * time.Minute
	}

	return config
}

func NewLoadTester(config Config) (*LoadTester, error) {
	// Parse faucet private key
	faucetKeyBytes, err := hex.DecodeString(FaucetPrivateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode faucet private key: %v", err)
	}

	// Extract the Ed25519 seed (last 32 bytes)
	faucetSeed := faucetKeyBytes[len(faucetKeyBytes)-32:]
	faucetPrivateKey := ed25519.NewKeyFromSeed(faucetSeed)
	faucetPublicKey := base58.Encode(faucetPrivateKey.Public().(ed25519.PublicKey))

	ctx, cancel := context.WithCancel(context.Background())

	// Initialize logger
	logger, err := NewLogger(config)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create logger: %v", err)
	}

	tester := &LoadTester{
		config:                config,
		ctx:                   ctx,
		cancel:                cancel,
		logger:                logger,
		faucetPrivateKey:      faucetPrivateKey,
		faucetPublicKey:       faucetPublicKey,
		fundingStartTime:      time.Now(),
		perAccountBaseNonce:   make(map[string]uint64),
		perAccountLocalCounts: make(map[string]uint64),
	}

	// Connect to gRPC server
	if err := tester.connect(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to server: %v", err)
	}

	// Generate accounts
	if err := tester.generateAccounts(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to generate accounts: %v", err)
	}

	// Fund accounts
	if err := tester.fundAccounts(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to fund accounts: %v", err)
	}

	return tester, nil
}

func (lt *LoadTester) connect() error {
	cfg := client.Config{Endpoint: lt.config.ServerAddress}
	client, err := client.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create client: %v", err)
	}
	lt.client = client

	return nil
}

func (lt *LoadTester) generateAccounts() error {
	lt.accounts = make([]Account, lt.config.AccountCount)

	for i := 0; i < lt.config.AccountCount; i++ {
		publicKey, privateKey, err := ed25519.GenerateKey(crand.Reader)
		if err != nil {
			return fmt.Errorf("failed to generate key pair for account %d: %v", i, err)
		}

		userId := int64(rand.Intn(100000000))
		address := hashStringToBase58(strconv.FormatInt(userId, 10))
		jwt := generateJwt(userId)
		publicKeyHex := base58.Encode(publicKey)
		proofRes, err := generateZkProof(strconv.FormatInt(userId, 10), address, publicKeyHex, jwt)
		if err != nil {
			return fmt.Errorf("failed to generate zk proof for account %d: %v", i, err)
		}
		zkPub := proofRes.Data.PublicInput
		zkProof := proofRes.Data.Proof

		lt.accounts[i] = Account{
			PublicKey:  publicKeyHex,
			PrivateKey: privateKey,
			Nonce:      0,
			Balance:    0,
			Address:    address,
			ZkProof:    zkProof,
			ZkPub:      zkPub,
		}
	}

	lt.logger.LogInfo("Generated %d accounts", lt.config.AccountCount)
	return nil
}

func hashStringToBase58(input string) string {
	sum := sha256.Sum256([]byte(input))
	return base58.Encode(sum[:])
}

func generateJwt(userID int64) string {
	exp := time.Now().UTC().Add(time.Duration(24) * time.Hour).Unix()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &SessionTokenClaims{
		TokenId:   strconv.FormatInt(userID, 10),
		UserId:    userID,
		Username:  strconv.FormatInt(userID, 10),
		ExpiresAt: exp,
	})
	signedToken, _ := token.SignedString([]byte(JwtSecret))
	return signedToken
}

// generateZkProof generates a ZK proof by calling the external service
func generateZkProof(userID, address, ephemeralPK, jwt string) (*ProveResponse, error) {
	url := ZkVerifyUrl + "/prove"
	// Create request body
	requestBody := map[string]string{
		"user_id":      userID,
		"address":      address,
		"ephemeral_pk": ephemeralPK,
		"jwt":          jwt,
	}

	// Convert to JSON
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %v", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")

	// Send request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("prove request failed: %d %s - %s",
			resp.StatusCode, resp.Status, string(body))
	}

	// Parse response
	var proveResp ProveResponse
	if err := json.NewDecoder(resp.Body).Decode(&proveResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return &proveResp, nil
}

func (lt *LoadTester) fundAccounts() error {
	lt.logger.LogInfo("Funding %d accounts with %d tokens each...", lt.config.AccountCount, lt.config.FundAmount)

	// Sequential funding to avoid duplicate nonce issues
	for i := range lt.accounts {
		if err := lt.fundAccount(i); err != nil {
			lt.logger.LogError("Failed to fund account %d: %v", i, err)
			// Continue with other accounts even if one fails
		}
		// Small delay between funding transactions to avoid conflicts
		time.Sleep(300 * time.Millisecond)
	}

	lt.logger.LogInfo("Account funding completed")
	return nil
}

func (lt *LoadTester) fundAccount(accountIndex int) error {
	account := &lt.accounts[accountIndex]

	// Retry logic for funding
	maxRetries := 5
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Single-flight faucet send
		lt.faucetSendMu.Lock()
		// Serialize faucet nonce to avoid duplicates
		nextNonce, err := lt.getNextFaucetNonce()
		if err != nil {
			lt.faucetSendMu.Unlock()
			if attempt == maxRetries {
				return fmt.Errorf("failed to get faucet nonce after %d attempts: %v", maxRetries, err)
			}
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			continue
		}
		amount := uint256.NewInt(lt.config.FundAmount)
		textData := fmt.Sprintf("Funding account %d", accountIndex)
		extraInfo := map[string]string{
			"type": "funding",
		}
		unsigned, err := client.BuildTransferTx(client.TxTypeFaucet, lt.faucetPublicKey, account.Address, amount, nextNonce, uint64(time.Now().Unix()), textData, extraInfo, "", "")
		if err != nil {
			return fmt.Errorf("failed to build transfer tx: %v", err)
		}

		// Sign transaction
		signedRaw, err := client.SignTx(unsigned, []byte{}, lt.faucetPrivateKey.Seed())
		if err != nil {
			return fmt.Errorf("failed to sign funding transaction: %v", err)
		}

		if !client.Verify(unsigned, signedRaw.Sig) {
			return fmt.Errorf("self verify failed")
		}

		// Send transaction
		resp, err := lt.client.AddTx(lt.ctx, signedRaw)
		if err != nil {
			// rollback allocated faucet nonce and retry
			lt.rollbackFaucetNonce(nextNonce)
			lt.faucetSendMu.Unlock()
			if attempt == maxRetries {
				return fmt.Errorf("failed to send funding transaction after %d attempts: %v", maxRetries, err)
			}
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			continue
		}

		if !resp.Ok {
			// Check if it's a nonce error and retry
			if attempt < maxRetries && (resp.Error != "" && contains(resp.Error, "nonce")) {
				lt.rollbackFaucetNonce(nextNonce)
				lt.logger.LogInfo("Nonce conflict for account %d, retrying... (attempt %d/%d)",
					accountIndex, attempt, maxRetries)
				lt.faucetSendMu.Unlock()
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			lt.faucetSendMu.Unlock()
			return fmt.Errorf("funding transaction failed: %s", resp.Error)
		}

		// Wait for funding tx to be confirmed to ensure account exists before any send
		if resp.TxHash != "" {
			_ = lt.waitTxConfirmed(resp.TxHash, 1*time.Second)
		} else {
			time.Sleep(500 * time.Millisecond)
		}

		// Update local bookkeeping
		account.Nonce = 0
		account.Balance = lt.config.FundAmount
		lt.faucetSendMu.Unlock()
		return nil
	}

	return fmt.Errorf("failed to fund account %d after %d attempts", accountIndex, maxRetries)
}

func (lt *LoadTester) refillAccount(accountIndex int) error {
	account := &lt.accounts[accountIndex]

	// Get serialized faucet next nonce
	lt.faucetSendMu.Lock()
	nextNonce, err := lt.getNextFaucetNonce()
	if err != nil {
		lt.faucetSendMu.Unlock()
		return fmt.Errorf("failed to get faucet nonce for refill: %v", err)
	}

	amount := uint256.NewInt(lt.config.FundAmount)
	textData := fmt.Sprintf("Refilling account %d", accountIndex)
	extraInfo := map[string]string{
		"type": "refilling",
	}
	unsigned, err := client.BuildTransferTx(client.TxTypeFaucet, lt.faucetPublicKey, account.Address, amount, nextNonce, uint64(time.Now().Unix()), textData, extraInfo, "", "")
	if err != nil {
		return fmt.Errorf("failed to build refill transaction: %v", err)
	}

	// Sign transaction
	signedRaw, err := client.SignTx(unsigned, []byte{}, lt.faucetPrivateKey.Seed())
	if err != nil {
		return fmt.Errorf("failed to sign refill transaction: %v", err)
	}

	if !client.Verify(unsigned, signedRaw.Sig) {
		return fmt.Errorf("self verify failed")
	}

	// Send transaction
	resp, err := lt.client.AddTx(lt.ctx, signedRaw)
	if err != nil {
		lt.rollbackFaucetNonce(nextNonce)
		lt.faucetSendMu.Unlock()
		return fmt.Errorf("failed to send refill transaction: %v", err)
	}

	if !resp.Ok {
		if contains(resp.Error, "nonce") {
			lt.rollbackFaucetNonce(nextNonce)
		}
		lt.faucetSendMu.Unlock()
		return fmt.Errorf("refill transaction failed: %s", resp.Error)
	}

	// Wait for transaction to be confirmed to ensure balance available
	if resp.TxHash != "" {
		_ = lt.waitTxConfirmed(resp.TxHash, 1*time.Second)
	} else {
		time.Sleep(500 * time.Millisecond)
	}

	lt.logger.LogInfo("Refilled account %d (%s...): %d tokens",
		accountIndex, account.Address[:8], lt.config.FundAmount)

	lt.faucetSendMu.Unlock()
	return nil
}

func (lt *LoadTester) Run() error {
	// Log test configuration
	if lt.config.RunMinutes > 0 {
		lt.logger.LogInfo("Starting load test: %d accounts, %d tx/s, switch after %d txs, run for %d minutes",
			lt.config.AccountCount, lt.config.TxPerSecond, lt.config.SwitchAfterTx, lt.config.RunMinutes)
	} else {
		lt.logger.LogInfo("Starting load test: %d accounts, %d tx/s, switch after %d txs",
			lt.config.AccountCount, lt.config.TxPerSecond, lt.config.SwitchAfterTx)
	}

	// Set test start time when actually starting to send transactions
	lt.testStartTime = time.Now()

	// Calculate interval per worker
	workers := lt.config.Workers
	if workers <= 0 {
		workers = 1
	}
	perWorkerRate := lt.config.TxPerSecond / workers
	if perWorkerRate <= 0 {
		perWorkerRate = 1
	}
	interval := time.Second / time.Duration(perWorkerRate)
	// Absolute scheduling to avoid tick coalescing/bursts
	nextSend := time.Now().Add(interval)

	// Create ticker for real-time metrics logging (every 10 seconds)
	lt.metricsTicker = time.NewTicker(10 * time.Second)
	defer lt.metricsTicker.Stop()

	// TPS per-second ticker
	// per-second TPS tracking removed

	// Setup timeout if duration is specified
	var timeout <-chan time.Time
	if lt.config.Duration > 0 {
		timeout = time.After(lt.config.Duration)
	}

	currentAccountIndex := 0
	txCountForCurrentAccount := 0

	for {
		select {
		case <-lt.ctx.Done():
			return nil
		case <-timeout:
			if lt.config.RunMinutes > 0 {
				lt.logger.LogInfo("Test completed after %d minutes", lt.config.RunMinutes)
			} else {
				lt.logger.LogInfo("Test duration completed")
			}
			// Print final statistics before quitting
			lt.PrintStats()
			os.Exit(0)
			return nil
		case <-lt.metricsTicker.C:
			// Log real-time metrics every 10 seconds in background
			go lt.logRealTimeMetrics()
		case <-time.After(time.Until(nextSend)):
			// Fan-out to N workers, each keeps its own schedule/account slice
			var wg sync.WaitGroup
			wg.Add(workers)
			for w := 0; w < workers; w++ {
				go func(workerID int) {
					defer wg.Done()
					idx := (currentAccountIndex + workerID) % lt.config.AccountCount
					lt.sendTransaction(idx)
					atomic.AddInt64(&lt.totalTxsSent, 1)
				}(w)
			}
			wg.Wait()

			// Update switching once per batch
			txCountForCurrentAccount += workers
			if txCountForCurrentAccount >= lt.config.SwitchAfterTx {
				currentAccountIndex = (currentAccountIndex + 1) % lt.config.AccountCount
				txCountForCurrentAccount = 0
			}

			// Schedule next send, skip backlog to avoid bursts
			nextSend = nextSend.Add(interval)
			now := time.Now()
			if nextSend.Before(now) {
				nextSend = now.Add(interval)
			}
			// per-second TPS tracking removed
		}
	}
}

func (lt *LoadTester) sendTransaction(accountIndex int) {
	account := &lt.accounts[accountIndex]

	// Check account balance first
	accountResp, err := lt.client.GetAccount(lt.ctx, account.Address)
	if err != nil {
		lt.logger.LogError("Failed to get account info for account %d: %v", accountIndex, err)
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		return
	}

	var currentBalance uint64
	fmt.Sscanf(accountResp.Balance.String(), "%d", &currentBalance)

	// Check if account has sufficient balance
	if currentBalance < lt.config.TransferAmount {
		lt.logger.LogInfo("Insufficient balance for account %d: have %d, need %d. Refilling...",
			accountIndex, currentBalance, lt.config.TransferAmount)

		// Try to refill the account
		if err := lt.refillAccount(accountIndex); err != nil {
			lt.logger.LogError("Failed to refill account %d: %v", accountIndex, err)
			atomic.AddInt64(&lt.totalTxsFailed, 1)
			return
		}

		// Update local balance after refill
		account.Balance = lt.config.FundAmount
	}

	// Determine nonce using per-account base + local counter
	nextNonce, err := lt.nextAccountNonce(account.Address)
	if err != nil {
		lt.logger.LogError("Failed to get nonce for account %d: %v", accountIndex, err)
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		return
	}

	// Choose random recipient (different from sender)
	recipientIndex := (accountIndex + 1) % lt.config.AccountCount
	recipient := &lt.accounts[recipientIndex]

	amount := uint256.NewInt(lt.config.TransferAmount)
	textData := fmt.Sprintf("Transfer from account %d to %d", accountIndex, recipientIndex)
	extraInfo := map[string]string{
		"type": "transfer",
	}
	unsigned, err := client.BuildTransferTx(client.TxTypeTransfer, account.Address, recipient.Address, amount, nextNonce, uint64(time.Now().Unix()), textData, extraInfo, account.ZkProof, account.ZkPub)
	if err != nil {
		lt.logger.LogError("Failed to build transfer transaction for account %d: %v", accountIndex, err)
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		return
	}

	// Sign transaction
	accountPublicKey, err := base58.Decode(account.PublicKey)
	if err != nil {
		lt.logger.LogError("Failed to decode from public key: %v", err)
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		return
	}
	signedRaw, err := client.SignTx(unsigned, accountPublicKey, account.PrivateKey.Seed())
	if err != nil {
		lt.logger.LogError("Failed to sign transaction for account %d: %v", accountIndex, err)
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		return
	}

	// Send transaction
	resp, err := lt.client.AddTx(lt.ctx, signedRaw)
	if err != nil {
		lt.logger.LogError("Failed to send transaction for account %d: %v", accountIndex, err)
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		// Refresh nonce from node on any failure
		if current, nerr := lt.refreshAccountBaseNonce(account.Address); nerr == nil {
			account.Nonce = current + 1
		}
		return
	}

	if resp.Ok {
		atomic.AddInt64(&lt.totalTxsSuccess, 1)
		// Update account nonce and balance
		account.Nonce = nextNonce
		account.Balance = currentBalance - lt.config.TransferAmount
	} else {
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		// On any failure, refresh base nonce for this account
		if current, nerr := lt.refreshAccountBaseNonce(account.Address); nerr == nil {
			account.Nonce = current + 1
		}
		lt.logger.LogError("Transaction failed for account %d: %s", accountIndex, resp.Error)
	}
}

func (lt *LoadTester) Stop() {
	lt.cancel()
}

func (lt *LoadTester) Close() {
	if lt.client.Conn() != nil {
		lt.client.Conn().Close()
	}
	if lt.logger != nil {
		lt.logger.Close()
	}
}

func (lt *LoadTester) PrintStats() {
	totalTxs := atomic.LoadInt64(&lt.totalTxsSent)
	successTxs := atomic.LoadInt64(&lt.totalTxsSuccess)
	failedTxs := atomic.LoadInt64(&lt.totalTxsFailed)

	// Use logger to write final stats
	lt.logger.LogFinalStats(totalTxs, successTxs, failedTxs, lt.testStartTime, lt.config)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (stringIndex(s, sub) >= 0)
}

// waitTxConfirmed polls tx status until CONFIRMED/FINALIZED or timeout
func (lt *LoadTester) waitTxConfirmed(hash string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("waitTxConfirmed timeout for %s", hash)
		}
		info, err := lt.client.GetTxByHash(lt.ctx, hash)
		if err == nil {
			// 1 = CONFIRMED, 2 = FINALIZED
			if info.Status == 1 || info.Status == 2 {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func stringIndex(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func (lt *LoadTester) refreshAccountBaseNonce(address string) (uint64, error) {
	current, err := lt.client.GetCurrentNonce(lt.ctx, address, "pending")
	if err != nil {
		return 0, err
	}
	lt.nonceMutex.Lock()
	lt.perAccountBaseNonce[address] = current
	lt.perAccountLocalCounts[address] = 0
	lt.nonceMutex.Unlock()
	return current, nil
}

func (lt *LoadTester) nextAccountNonce(address string) (uint64, error) {
	lt.nonceMutex.Lock()
	base, ok := lt.perAccountBaseNonce[address]
	count := lt.perAccountLocalCounts[address]
	lt.nonceMutex.Unlock()

	if !ok {
		if _, err := lt.refreshAccountBaseNonce(address); err != nil {
			return 0, err
		}
		lt.nonceMutex.Lock()
		base = lt.perAccountBaseNonce[address]
		count = lt.perAccountLocalCounts[address]
		lt.nonceMutex.Unlock()
	}

	next := base + count + 1
	lt.nonceMutex.Lock()
	lt.perAccountLocalCounts[address] = count + 1
	lt.nonceMutex.Unlock()
	return next, nil
}

// Faucet nonce helpers
func (lt *LoadTester) getNextFaucetNonce() (uint64, error) {
	lt.faucetNonceMu.Lock()
	defer lt.faucetNonceMu.Unlock()
	if !lt.faucetNonceInit {
		current, err := lt.client.GetCurrentNonce(lt.ctx, lt.faucetPublicKey, "pending")
		if err != nil {
			return 0, err
		}
		lt.faucetNonce = current + 1
		lt.faucetNonceInit = true
		return lt.faucetNonce, nil
	}
	lt.faucetNonce++
	return lt.faucetNonce, nil
}

func (lt *LoadTester) rollbackFaucetNonce(allocated uint64) {
	lt.faucetNonceMu.Lock()
	defer lt.faucetNonceMu.Unlock()
	if !lt.faucetNonceInit {
		return
	}
	if lt.faucetNonce == allocated && lt.faucetNonce > 0 {
		lt.faucetNonce--
	}
}
