package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	crand "crypto/rand"
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
	"regexp"
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

type ErrType int

const (
	NoneErr ErrType = iota
	DuplicateErr
	BalanceErr
	NonceErr
	RequestErr
)

const (
	clientErrCode          = "client_error"
	unknownErrCode         = "unknown_error"
	deduplicateNonceWindow = 200
)

// Configuration
type Config struct {
	ServerAddress        string
	AccountCount         int
	TxPerSecond          int
	FundAmount           uint64
	TransferAmount       uint64
	Duration             time.Duration
	RunMinutes           int   // Run for specified minutes, then stop
	TotalTransactions    int64 // Total transactions to send, then stop (priority over time)
	TransferByPrivateKey bool
	ErrDuplicate         int
	ErrBalance           int
	ErrNonce             int
	ErrRequest           int
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

	// Per-account mutexes
	accountMutexes sync.Map // map[int]*sync.Mutex

	// Global nonce
	globalNonce uint64
	nonceMu     sync.RWMutex

	// JSON Report tracking
	snapshots      []Snapshot
	snapshotsMutex sync.Mutex
	tpsHistory     [][]int64
	errorCounts    map[string]int64
	errorMutex     sync.Mutex

	// Wait group for goroutines
	wg sync.WaitGroup
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

type Snapshot struct {
	Ts          int64   `json:"ts"`
	TotalSent   int64   `json:"total_sent"`
	SuccessTxs  int64   `json:"success_txs"`
	FailedTxs   int64   `json:"failed_txs"`
	TPS         int64   `json:"tps"`
	SuccessRate float64 `json:"success_rate"`
}

// Faucet private key from genesis (same as in TypeScript tests)
const (
	FaucetPrivateKeyHex = "302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee"
	JwtSecret           = "defaultencryptionkey"
	ZkVerifyUrl         = "http://localhost:8282"
)

// logRealTimeMetrics logs current system metrics and transaction stats
func (lt *LoadTester) logRealTimeMetrics() {
	// Get current transaction stats
	totalTxs := atomic.LoadInt64(&lt.totalTxsSent)
	successTxs := atomic.LoadInt64(&lt.totalTxsSuccess)
	failedTxs := atomic.LoadInt64(&lt.totalTxsFailed)

	// Use logger to write real-time metrics
	lt.logger.LogRealTimeMetrics(totalTxs, successTxs, failedTxs, lt.testStartTime, lt.config)

	// Create snapshot
	lt.captureSnapshot(totalTxs, successTxs, failedTxs, false, time.Time{})
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

	runFinished := make(chan struct{}, 1)
	// Start load testing in a goroutine
	go func() {
		tester.Run()
		runFinished <- struct{}{}
	}()

	// Wait for signal or completion
	select {
	case <-sigChan:
		tester.logger.LogInfo("Received shutdown signal, stopping load test...")
		tester.Stop()
	case <-tester.ctx.Done():
		tester.logger.LogInfo("Stopping... waiting for all transactions to finish...")
	case <-runFinished:
		tester.logger.LogInfo("Stopping... waiting for all transactions to finish...")
	}

	endTime := time.Now()
	tester.wg.Wait()

	// Last snapshot
	totalTxs := atomic.LoadInt64(&tester.totalTxsSent)
	successTxs := atomic.LoadInt64(&tester.totalTxsSuccess)
	failedTxs := atomic.LoadInt64(&tester.totalTxsFailed)
	tester.captureSnapshot(totalTxs, successTxs, failedTxs, true, endTime)

	// Print final statistics
	tester.PrintStats(endTime)
}

func parseFlags() Config {
	var config Config

	flag.StringVar(&config.ServerAddress, "server", "127.0.0.1:9001", "gRPC server address")
	flag.IntVar(&config.AccountCount, "accounts", 100, "Number of accounts to create")
	flag.IntVar(&config.TxPerSecond, "rate", 2000, "Transactions per second")
	flag.Uint64Var(&config.FundAmount, "fund", 10000000000, "Amount to fund each account")
	flag.Uint64Var(&config.TransferAmount, "amount", 100, "Amount per transfer transaction")
	flag.DurationVar(&config.Duration, "duration", 0, "Test duration (0 = run indefinitely)")
	flag.IntVar(&config.RunMinutes, "minutes", 0, "Run for specified minutes, then stop (0 = run indefinitely)")
	flag.Int64Var(&config.TotalTransactions, "total_txs", 0, "Total transactions to send, then stop (0 = run indefinitely, priority over time limits)")
	flag.BoolVar(&config.TransferByPrivateKey, "use-key", true, "Use private key for transfers instead of zk proof")
	flag.IntVar(&config.ErrBalance, "err-balance", 0, "Number of balance error txs per second")
	flag.IntVar(&config.ErrNonce, "err-nonce", 0, "Number of nonce error txs per second")
	flag.IntVar(&config.ErrDuplicate, "err-duplicate", 0, "Number of duplicate error txs per second")
	flag.IntVar(&config.ErrRequest, "err-request", 0, "Number of invalid request error txs per second")

	flag.Parse()

	// Validate error flags
	totalErrors := config.ErrBalance + config.ErrNonce + config.ErrDuplicate + config.ErrRequest
	if totalErrors > config.TxPerSecond {
		log.Fatalf("Total error transactions per second (%d) exceed total TPS (%d)", totalErrors, config.TxPerSecond)
	}
	if config.ErrDuplicate > (config.TxPerSecond - 1) {
		log.Fatalf("Duplicate error transactions per second (%d) exceed limit (%d)", config.ErrDuplicate, config.TxPerSecond-1)
	}

	// Priority: total transactions > time limits
	if config.TotalTransactions > 0 {
		config.Duration = 0
		config.RunMinutes = 0
	} else if config.RunMinutes > 0 {
		config.Duration += time.Duration(config.RunMinutes) * time.Minute
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
		config:           config,
		ctx:              ctx,
		cancel:           cancel,
		logger:           logger,
		faucetPrivateKey: faucetPrivateKey,
		faucetPublicKey:  faucetPublicKey,
		fundingStartTime: time.Now(),
		snapshots:        make([]Snapshot, 0),
		tpsHistory:       make([][]int64, 0),
		errorCounts:      make(map[string]int64),
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

		publicKeyHex := base58.Encode(publicKey)
		address := publicKeyHex

		var zkPub, zkProof string
		if !lt.config.TransferByPrivateKey {
			userId := int64(rand.Intn(100000000))
			address = client.GenerateAddress(strconv.FormatInt(userId, 10))
			jwt := generateJwt(userId)
			proofRes, err := generateZkProof(strconv.FormatInt(userId, 10), address, publicKeyHex, jwt)
			if err != nil {
				return fmt.Errorf("failed to generate zk proof for account %d: %v", i, err)
			}
			zkPub = proofRes.Data.PublicInput
			zkProof = proofRes.Data.Proof
		}

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
	lt.refreshGlobalNonce()

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
		amount := uint256.NewInt(lt.config.FundAmount)
		textData := fmt.Sprintf("Funding account %d", accountIndex)
		extraInfo := map[string]string{
			"type": "funding",
		}
		nonce := lt.getGlobalNonce()

		unsigned, err := client.BuildTransferTx(client.TxTypeTransferByKey, lt.faucetPublicKey, account.Address, amount, nonce, uint64(time.Now().Unix()), textData, extraInfo, "", "")
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
			if attempt == maxRetries {
				return fmt.Errorf("failed to send funding transaction after %d attempts: %v", maxRetries, err)
			}
			lt.refreshGlobalNonce()
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			continue
		}

		if !resp.Ok {
			// Check if it's a nonce error and retry
			if attempt < maxRetries && (resp.Error != "" && contains(resp.Error, "nonce")) {
				lt.refreshGlobalNonce()
				lt.logger.LogInfo("Nonce conflict for account %d, retrying... (attempt %d/%d)", accountIndex, attempt, maxRetries)
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
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
		return nil
	}

	return fmt.Errorf("failed to fund account %d after %d attempts", accountIndex, maxRetries)
}

func (lt *LoadTester) refillAccount(accountIndex int, globalNonce uint64) error {
	account := &lt.accounts[accountIndex]
	amount := uint256.NewInt(lt.config.FundAmount)
	textData := fmt.Sprintf("Refilling account %d", accountIndex)
	extraInfo := map[string]string{
		"type": "refilling",
	}

	unsigned, err := client.BuildTransferTx(client.TxTypeTransferByKey, lt.faucetPublicKey, account.Address, amount, globalNonce, uint64(time.Now().Unix()), textData, extraInfo, "", "")
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
		return fmt.Errorf("failed to send refill transaction: %v", err)
	}

	if !resp.Ok {
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

	return nil
}

func (lt *LoadTester) Run() {
	var timeout <-chan time.Time
	if lt.config.TotalTransactions > 0 {
		lt.logger.LogInfo("Starting load test: %d accounts, target %d tx/s, total transactions: %d",
			lt.config.AccountCount, lt.config.TxPerSecond, lt.config.TotalTransactions)
	} else if lt.config.Duration > 0 {
		timeout = time.After(lt.config.Duration)
		lt.logger.LogInfo("Starting load test: %d accounts, target %d tx/s, duration: %s", lt.config.AccountCount, lt.config.TxPerSecond, lt.config.Duration)
	} else {
		lt.logger.LogInfo("Starting load test: %d accounts, target %d tx/s", lt.config.AccountCount, lt.config.TxPerSecond)
	}

	lt.testStartTime = time.Now()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lt.metricsTicker = time.NewTicker(10 * time.Second)
	defer lt.metricsTicker.Stop()

	for {
		select {
		case <-lt.ctx.Done():
			return

		case <-timeout:
			if lt.config.RunMinutes > 0 {
				lt.logger.LogInfo("Test completed after %d minutes", lt.config.RunMinutes)
			} else {
				lt.logger.LogInfo("Test duration completed")
			}
			// Stop gracefully by cancelling context
			lt.Stop()
			return

		case <-lt.metricsTicker.C:
			// Log real-time metrics every 10 seconds in background
			go lt.logRealTimeMetrics()

		case <-ticker.C:
			globalNonce, err := lt.refreshGlobalNonce()
			if err != nil {
				lt.logger.LogError("Failed to refresh global nonce: %v", err)
				continue
			}
			lt.logger.LogInfo("Global nonce: %d", globalNonce)

			// Check if total transactions target reached
			if lt.config.TotalTransactions > 0 {
				currentSent := atomic.LoadInt64(&lt.totalTxsSent)
				if currentSent >= lt.config.TotalTransactions {
					lt.logger.LogInfo("Target reached (%d). Stopping generation loop.", currentSent)
					return
				}
			}

			go lt.runTickWithNonce(globalNonce)
		}
	}
}

// Run a single tick sending transactions with the given nonce
func (lt *LoadTester) runTickWithNonce(nonce uint64) {
	select {
	case <-lt.ctx.Done():
		return
	default:
	}

	accountCount := lt.config.AccountCount
	targetTPS := lt.config.TxPerSecond

	errDuplicateCount := lt.config.ErrDuplicate
	if errDuplicateCount > 0 {
		errDuplicateCount++ // Need at least one non-duplicate tx
	}
	errBalanceCount := lt.config.ErrBalance
	errNonceCount := lt.config.ErrNonce
	errRequestCount := lt.config.ErrRequest

	sent := 0
	offset := 1

	for sent < targetTPS {
		for i := 0; i < accountCount && sent < targetTPS; i++ {
			sender := i
			receiver := (i + offset) % accountCount
			var txErr ErrType

			switch {
			case errDuplicateCount > 0:
				txErr = DuplicateErr
				errDuplicateCount--

			case errBalanceCount > 0:
				txErr = BalanceErr
				errBalanceCount--

			case errNonceCount > 0:
				txErr = NonceErr
				errNonceCount--

			case errRequestCount > 0:
				txErr = RequestErr
				errRequestCount--

			default:
				txErr = NoneErr

			}

			// Check if we have reached the total transaction limit
			if !lt.incrementAndCheckLimit() {
				return
			}

			lt.wg.Add(1)
			go func() {
				defer lt.wg.Done()
				lt.sendTransaction(sender, receiver, nonce, txErr)
			}()
			sent++
		}
		offset++
		if offset >= accountCount {
			offset = 1
		}
	}
}

// Send a transfer transaction from sender to receiver
func (lt *LoadTester) sendTransaction(senderIdx, receiverIdx int, nonce uint64, errType ErrType) {
	select {
	case <-lt.ctx.Done():
		return
	default:
	}

	sender := &lt.accounts[senderIdx]
	receiver := &lt.accounts[receiverIdx]
	amount := uint256.NewInt(lt.config.TransferAmount)
	timestamp := uint64(time.Now().Unix())
	textData := fmt.Sprintf("Transfer from account %d to %d at %d", senderIdx, receiverIdx, rand.Int63n(1_000_000_000_000_000_000))
	extraInfo := map[string]string{"type": "transfer"}
	transferType := client.TxTypeTransferByZk
	if lt.config.TransferByPrivateKey {
		transferType = client.TxTypeTransferByKey
	}

	switch errType {
	case DuplicateErr:
		senderIdx = 0
		receiverIdx = 1
		sender = &lt.accounts[senderIdx]
		receiver = &lt.accounts[receiverIdx]
		textData = "Duplicate transaction test"

	case BalanceErr:
		amount = uint256.NewInt(lt.config.FundAmount * 2)

	case NonceErr:
		nonce -= deduplicateNonceWindow

	case RequestErr:
		textData = "Injection payload eval(1)  {{ console.log('hacked'); }}"

	}

	mu := lt.getAccountMutex(senderIdx)
	mu.Lock()
	balance := sender.Balance
	if balance < lt.config.TransferAmount {
		lt.logger.LogInfo("Account %d has low balance (%d), refilling...", senderIdx, balance)

		if err := lt.refillAccount(senderIdx, nonce); err != nil {
			lt.logger.LogError("Refill failed for account %d: %v", senderIdx, err)
			atomic.AddInt64(&lt.totalTxsFailed, 1)
			lt.trackError(clientErrCode)
			return
		}
		sender.Balance += lt.config.FundAmount
	}
	mu.Unlock()

	unsigned, err := client.BuildTransferTx(
		transferType,
		sender.Address,
		receiver.Address,
		amount,
		nonce,
		timestamp,
		textData,
		extraInfo,
		sender.ZkProof,
		sender.ZkPub,
	)
	if err != nil {
		lt.logger.LogError("Build tx failed: %v", err)
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		lt.trackError(clientErrCode)
		return
	}

	pubKey, _ := base58.Decode(sender.PublicKey)
	signedRaw, err := client.SignTx(unsigned, pubKey, sender.PrivateKey.Seed())
	if err != nil {
		lt.logger.LogError("Sign tx failed: %v", err)
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		lt.trackError(clientErrCode)
		return
	}

	mu.Lock()
	sender.Balance -= lt.config.TransferAmount
	mu.Unlock()

	resp, err := lt.client.AddTx(lt.ctx, signedRaw)
	if err != nil {
		// If transaction failed due to context cancellation, do not count it as failed
		if contains(err.Error(), "context canceled") || lt.ctx.Err() == context.Canceled {
			atomic.AddInt64(&lt.totalTxsSent, -1)
			return
		}

		lt.logger.LogError("Send tx failed: %v", err)
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		lt.trackError(extractErrorCode(err.Error()))
		mu.Lock()
		sender.Balance += lt.config.TransferAmount
		mu.Unlock()
		return
	}

	if resp.Ok {
		atomic.AddInt64(&lt.totalTxsSuccess, 1)
	} else {
		lt.logger.LogError("Tx failed for %d->%d: %s", senderIdx, receiverIdx, resp.Error)
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		lt.trackError(extractErrorCode(resp.Error))

		mu.Lock()
		sender.Balance += lt.config.TransferAmount
		mu.Unlock()
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

func (lt *LoadTester) PrintStats(endTime time.Time) {
	totalTxs := atomic.LoadInt64(&lt.totalTxsSent)
	successTxs := atomic.LoadInt64(&lt.totalTxsSuccess)
	failedTxs := atomic.LoadInt64(&lt.totalTxsFailed)

	// Use logger to write final stats
	lt.logger.LogFinalStats(totalTxs, successTxs, failedTxs, lt.testStartTime, lt.config, endTime)

	// Generate JSON report and HTML
	lt.generateReport(totalTxs, successTxs, failedTxs, endTime)
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

// Get mutex for each account
func (lt *LoadTester) getAccountMutex(index int) *sync.Mutex {
	if m, ok := lt.accountMutexes.Load(index); ok {
		return m.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := lt.accountMutexes.LoadOrStore(index, mu)
	return actual.(*sync.Mutex)
}

// Get global nonce for account
func (lt *LoadTester) getGlobalNonce() uint64 {
	lt.nonceMu.RLock()
	defer lt.nonceMu.RUnlock()
	return lt.globalNonce
}

// Refresh global nonce from node
func (lt *LoadTester) refreshGlobalNonce() (uint64, error) {
	lt.nonceMu.Lock()
	defer lt.nonceMu.Unlock()
	nonce, err := lt.client.GetCurrentNonce(lt.ctx, lt.faucetPublicKey, "pending")
	if err != nil {
		return 0, err
	}
	lt.globalNonce = nonce
	return lt.globalNonce, nil
}

// Increment and check total sent transactions
func (lt *LoadTester) incrementAndCheckLimit() bool {
	newTotal := atomic.AddInt64(&lt.totalTxsSent, 1)

	if (lt.config.TotalTransactions > 0) && (newTotal > lt.config.TotalTransactions) {
		atomic.AddInt64(&lt.totalTxsSent, -1)
		return false
	}

	return true
}

// captureSnapshot creates a snapshot of current test state
func (lt *LoadTester) captureSnapshot(totalTxs int64, successTxs int64, failedTxs int64, isLastCapture bool, endTime time.Time) {
	lt.snapshotsMutex.Lock()
	defer lt.snapshotsMutex.Unlock()

	now := time.Now()
	if isLastCapture {
		now = endTime
	}
	ts := now.Unix()

	testDuration := now.Sub(lt.testStartTime)
	actualRate := float64(totalTxs) / testDuration.Seconds()
	currentTPS := int64(actualRate)

	successRate := float64(0)
	if totalTxs > 0 {
		successRate = float64(successTxs) / float64(totalTxs) * 100
	}

	snapshot := Snapshot{
		Ts:          ts,
		TotalSent:   totalTxs,
		SuccessTxs:  successTxs,
		FailedTxs:   failedTxs,
		TPS:         currentTPS,
		SuccessRate: successRate,
	}

	lt.snapshots = append(lt.snapshots, snapshot)
	lt.tpsHistory = append(lt.tpsHistory, []int64{ts, currentTPS})
}

// trackError increments error count for a given error type
func (lt *LoadTester) trackError(errType string) {
	lt.errorMutex.Lock()
	defer lt.errorMutex.Unlock()

	lt.errorCounts[errType]++
}

var codeRegex = regexp.MustCompile(`"code"\s*:\s*"([^"]+)"`)

func extractErrorCode(errorMsg string) string {
	match := codeRegex.FindStringSubmatch(errorMsg)
	if len(match) == 2 {
		return match[1]
	}
	return unknownErrCode
}

// generateReport creates and saves the JSON and HTML reports
func (lt *LoadTester) generateReport(totalTxs, successTxs, failedTxs int64, endTime time.Time) {
	GenerateAndSaveReports(
		lt.config,
		lt.testStartTime,
		endTime,
		totalTxs,
		successTxs,
		failedTxs,
		lt.tpsHistory,
		lt.errorCounts,
		lt.snapshots,
		lt.logger,
	)
}
