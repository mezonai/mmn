package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mr-tron/base58"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/mezonai/mmn/proto"
)

// Configuration
type Config struct {
	ServerAddress     string
	AccountCount      int
	TxPerSecond       int
	SwitchAfterTx     int
	FundAmount        uint64
	TransferAmount    uint64
	Duration          time.Duration
	RunMinutes        int // Run for specified minutes, then stop
	Workers           int // Number of concurrent send workers
}

// Account represents a test account
type Account struct {
	PublicKey  string
	PrivateKey ed25519.PrivateKey
	Nonce      uint64
	Balance    uint64
}

// SystemMetrics holds system resource metrics
type SystemMetrics struct {
	CPUPercent    float64
	MemoryPercent float64
	DiskRead      uint64
	DiskWrite     uint64
	NetworkRx     uint64
	NetworkTx     uint64
	Timestamp     time.Time
}

// LoadTester handles the load testing
type LoadTester struct {
	config     Config
	accounts   []Account
	client     pb.TxServiceClient
	accountClient pb.AccountServiceClient
	conn       *grpc.ClientConn
	ctx        context.Context
	cancel     context.CancelFunc
	
	// Statistics
	totalTxsSent    int64
	totalTxsSuccess int64
	totalTxsFailed  int64
	fundingStartTime time.Time
	testStartTime    time.Time
	
	// System metrics tracking
	lastMetrics     *SystemMetrics
	metricsTicker   *time.Ticker
	
	// Faucet account (hardcoded from genesis)
	faucetPrivateKey ed25519.PrivateKey
	faucetPublicKey  string
}

// Faucet private key from genesis (same as in TypeScript tests)
const faucetPrivateKeyHex = "302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee"

// collectSystemMetrics collects current system metrics
func collectSystemMetrics() (*SystemMetrics, error) {
	metrics := &SystemMetrics{
		Timestamp: time.Now(),
	}

	// CPU usage
	cpuPercent, err := cpu.Percent(0, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get CPU metrics: %v", err)
	}
	if len(cpuPercent) > 0 {
		metrics.CPUPercent = cpuPercent[0]
	}

	// Memory usage
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return nil, fmt.Errorf("failed to get memory metrics: %v", err)
	}
	metrics.MemoryPercent = memInfo.UsedPercent

	// Disk I/O
	diskStats, err := disk.IOCounters()
	if err != nil {
		return nil, fmt.Errorf("failed to get disk metrics: %v", err)
	}
	
	// Sum up all disk I/O
	for _, stat := range diskStats {
		metrics.DiskRead += stat.ReadBytes
		metrics.DiskWrite += stat.WriteBytes
	}

	// Network I/O
	netStats, err := net.IOCounters(false)
	if err != nil {
		return nil, fmt.Errorf("failed to get network metrics: %v", err)
	}
	
	if len(netStats) > 0 {
		metrics.NetworkRx = netStats[0].BytesRecv
		metrics.NetworkTx = netStats[0].BytesSent
	}

	return metrics, nil
}

// formatBytes formats bytes into human readable format
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// logRealTimeMetrics logs current system metrics and transaction stats
func (lt *LoadTester) logRealTimeMetrics() {
	// Get current metrics
	currentMetrics, err := collectSystemMetrics()
	if err != nil {
		log.Printf("Failed to collect system metrics: %v", err)
		return
	}

	// Get current transaction stats
	totalTxs := atomic.LoadInt64(&lt.totalTxsSent)
	successTxs := atomic.LoadInt64(&lt.totalTxsSuccess)
	failedTxs := atomic.LoadInt64(&lt.totalTxsFailed)
	
	// Calculate rates
	testDuration := time.Since(lt.testStartTime)
	actualRate := float64(totalTxs) / testDuration.Seconds()
	successRate := float64(successTxs) / float64(totalTxs) * 100
	if totalTxs == 0 {
		successRate = 0
	}

	// Peak/sustained removed

	// Calculate I/O deltas if we have previous metrics
	var diskReadDelta, diskWriteDelta, netRxDelta, netTxDelta uint64
	if lt.lastMetrics != nil {
		diskReadDelta = currentMetrics.DiskRead - lt.lastMetrics.DiskRead
		diskWriteDelta = currentMetrics.DiskWrite - lt.lastMetrics.DiskWrite
		netRxDelta = currentMetrics.NetworkRx - lt.lastMetrics.NetworkRx
		netTxDelta = currentMetrics.NetworkTx - lt.lastMetrics.NetworkTx
	}

	// Log metrics
	fmt.Printf("\n=== REAL-TIME METRICS [%s] ===\n", currentMetrics.Timestamp.Format("15:04:05"))
	fmt.Printf("Transactions: %d sent, %d success, %d failed (avg %.2f tx/s, %.1f%% success)\n", 
		totalTxs, successTxs, failedTxs, actualRate, successRate)
	
	// Show remaining time if using minutes option
	if lt.config.RunMinutes > 0 {
		elapsed := time.Since(lt.testStartTime)
		remaining := time.Duration(lt.config.RunMinutes)*time.Minute - elapsed
		if remaining > 0 {
			fmt.Printf("Time: %v elapsed, %v remaining\n", 
				elapsed.Round(time.Second), remaining.Round(time.Second))
		} else {
			fmt.Printf("Time: %v elapsed (test should stop soon)\n", 
				elapsed.Round(time.Second))
		}
	}
	
	fmt.Printf("System: CPU %.1f%%, RAM %.1f%%\n", 
		currentMetrics.CPUPercent, currentMetrics.MemoryPercent)
	fmt.Printf("I/O: Disk R/W %s/%s, Network Rx/Tx %s/%s\n", 
		formatBytes(diskReadDelta), formatBytes(diskWriteDelta),
		formatBytes(netRxDelta), formatBytes(netTxDelta))
	fmt.Printf("=====================================\n")

	// Update last metrics for next calculation
	lt.lastMetrics = currentMetrics
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
			log.Printf("Load test error: %v", err)
		}
	}()

	// Wait for signal or completion
	select {
	case <-sigChan:
		log.Println("Received shutdown signal, stopping load test...")
		tester.Stop()
	case <-tester.ctx.Done():
		log.Println("Load test completed")
	}

	// Print final statistics
	tester.PrintStats()
}

func parseFlags() Config {
	var config Config
	
	flag.StringVar(&config.ServerAddress, "server", "127.0.0.1:9001", "gRPC server address")
	flag.IntVar(&config.AccountCount, "accounts", 200, "Number of accounts to create")
	flag.IntVar(&config.TxPerSecond, "rate", 200, "Transactions per second")
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
	faucetKeyBytes, err := hex.DecodeString(faucetPrivateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode faucet private key: %v", err)
	}
	
	// Extract the Ed25519 seed (last 32 bytes)
	faucetSeed := faucetKeyBytes[len(faucetKeyBytes)-32:]
	faucetPrivateKey := ed25519.NewKeyFromSeed(faucetSeed)
	faucetPublicKey := base58.Encode(faucetPrivateKey.Public().(ed25519.PublicKey))
	
	ctx, cancel := context.WithCancel(context.Background())
	
	tester := &LoadTester{
		config:           config,
		ctx:              ctx,
		cancel:           cancel,
		faucetPrivateKey: faucetPrivateKey,
		faucetPublicKey:  faucetPublicKey,
		fundingStartTime: time.Now(),
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
	conn, err := grpc.Dial(lt.config.ServerAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to gRPC server: %v", err)
	}
	
	lt.conn = conn
	lt.client = pb.NewTxServiceClient(conn)
	lt.accountClient = pb.NewAccountServiceClient(conn)
	
	return nil
}

func (lt *LoadTester) generateAccounts() error {
	lt.accounts = make([]Account, lt.config.AccountCount)
	
	for i := 0; i < lt.config.AccountCount; i++ {
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return fmt.Errorf("failed to generate key pair for account %d: %v", i, err)
		}
		
		lt.accounts[i] = Account{
			PublicKey:  base58.Encode(publicKey),
			PrivateKey: privateKey,
			Nonce:      0,
			Balance:    0,
		}
	}
	
	log.Printf("Generated %d accounts", lt.config.AccountCount)
	return nil
}

func (lt *LoadTester) fundAccounts() error {
	log.Printf("Funding %d accounts with %d tokens each...", lt.config.AccountCount, lt.config.FundAmount)
	
	// Sequential funding to avoid duplicate nonce issues
	for i := range lt.accounts {
		if err := lt.fundAccount(i); err != nil {
			log.Printf("Failed to fund account %d: %v", i, err)
			// Continue with other accounts even if one fails
		}
		// Small delay between funding transactions to avoid conflicts
		time.Sleep(300 * time.Millisecond)
	}
	
	log.Println("Account funding completed")
	return nil
}

func (lt *LoadTester) fundAccount(accountIndex int) error {
	account := &lt.accounts[accountIndex]
	
	// Retry logic for funding
	maxRetries := 5
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Get current nonce for faucet
		nonceResp, err := lt.accountClient.GetCurrentNonce(lt.ctx, &pb.GetCurrentNonceRequest{
			Address: lt.faucetPublicKey,
			Tag:     "pending",
		})
		if err != nil {
			if attempt == maxRetries {
				return fmt.Errorf("failed to get faucet nonce after %d attempts: %v", maxRetries, err)
			}
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			continue
		}
		
		nextNonce := nonceResp.Nonce + 1
		
		// Create funding transaction
		txMsg := &pb.TxMsg{
			Type:      0, // Transfer type
			Sender:    lt.faucetPublicKey,
			Recipient: account.PublicKey,
			Amount:    fmt.Sprintf("%d", lt.config.FundAmount),
			Timestamp: uint64(time.Now().UnixNano() / int64(time.Millisecond)),
			TextData:  fmt.Sprintf("Funding account %d", accountIndex),
			Nonce:     nextNonce,
			ExtraInfo: "",
		}
		
		// Sign transaction
		signature, err := lt.signTransaction(txMsg, lt.faucetPrivateKey)
		if err != nil {
			return fmt.Errorf("failed to sign funding transaction: %v", err)
		}
		
		// Send transaction
		signedTx := &pb.SignedTxMsg{
			TxMsg:     txMsg,
			Signature: signature,
		}
		
		resp, err := lt.client.AddTx(lt.ctx, signedTx)
		if err != nil {
			if attempt == maxRetries {
				return fmt.Errorf("failed to send funding transaction after %d attempts: %v", maxRetries, err)
			}
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			continue
		}
		
		if !resp.Ok {
			// Check if it's a nonce error and retry
			if attempt < maxRetries && (resp.Error == "duplicate nonce" || 
				(resp.Error != "" && resp.Error == "duplicate nonce")) {
				log.Printf("Nonce conflict for account %d, retrying... (attempt %d/%d)", 
					accountIndex, attempt, maxRetries)
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			return fmt.Errorf("funding transaction failed: %s", resp.Error)
		}
		
		// Assume success locally to accelerate funding
		account.Nonce = 0
		account.Balance = lt.config.FundAmount
		log.Printf("Funded account %d (%s...): %d tokens", 
			accountIndex, account.PublicKey[:8], account.Balance)
		return nil
	}
	
	return fmt.Errorf("failed to fund account %d after %d attempts", accountIndex, maxRetries)
}

func (lt *LoadTester) refillAccount(accountIndex int) error {
	account := &lt.accounts[accountIndex]
	
	// Get current nonce for faucet
	nonceResp, err := lt.accountClient.GetCurrentNonce(lt.ctx, &pb.GetCurrentNonceRequest{
		Address: lt.faucetPublicKey,
		Tag:     "pending",
	})
	if err != nil {
		return fmt.Errorf("failed to get faucet nonce for refill: %v", err)
	}
	
	nextNonce := nonceResp.Nonce + 1
	
	// Create refill transaction
	txMsg := &pb.TxMsg{
		Type:      0, // Transfer type
		Sender:    lt.faucetPublicKey,
		Recipient: account.PublicKey,
		Amount:    fmt.Sprintf("%d", lt.config.FundAmount),
		Timestamp: uint64(time.Now().UnixNano() / int64(time.Millisecond)),
		TextData:  fmt.Sprintf("Refilling account %d", accountIndex),
		Nonce:     nextNonce,
		ExtraInfo: "",
	}
	
	// Sign transaction
	signature, err := lt.signTransaction(txMsg, lt.faucetPrivateKey)
	if err != nil {
		return fmt.Errorf("failed to sign refill transaction: %v", err)
	}
	
	// Send transaction
	signedTx := &pb.SignedTxMsg{
		TxMsg:     txMsg,
		Signature: signature,
	}
	
	resp, err := lt.client.AddTx(lt.ctx, signedTx)
	if err != nil {
		return fmt.Errorf("failed to send refill transaction: %v", err)
	}
	
	if !resp.Ok {
		return fmt.Errorf("refill transaction failed: %s", resp.Error)
	}
	
	// Wait for transaction to be processed
	time.Sleep(500 * time.Millisecond)
	
	log.Printf("Refilled account %d (%s...): %d tokens", 
		accountIndex, account.PublicKey[:8], lt.config.FundAmount)
	
	return nil
}

func (lt *LoadTester) Run() error {
	// Log test configuration
	if lt.config.RunMinutes > 0 {
		log.Printf("Starting load test: %d accounts, %d tx/s, switch after %d txs, run for %d minutes", 
			lt.config.AccountCount, lt.config.TxPerSecond, lt.config.SwitchAfterTx, lt.config.RunMinutes)
	} else {
		log.Printf("Starting load test: %d accounts, %d tx/s, switch after %d txs", 
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
				log.Printf("Test completed after %d minutes", lt.config.RunMinutes)
			} else {
				log.Println("Test duration completed")
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
	accountResp, err := lt.accountClient.GetAccount(lt.ctx, &pb.GetAccountRequest{
		Address: account.PublicKey,
	})
	if err != nil {
		log.Printf("Failed to get account info for account %d: %v", accountIndex, err)
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		return
	}
	
	var currentBalance uint64
	fmt.Sscanf(accountResp.Balance, "%d", &currentBalance)
	
	// Check if account has sufficient balance
	if currentBalance < lt.config.TransferAmount {
		log.Printf("Insufficient balance for account %d: have %d, need %d. Refilling...", 
			accountIndex, currentBalance, lt.config.TransferAmount)
		
		// Try to refill the account
		if err := lt.refillAccount(accountIndex); err != nil {
			log.Printf("Failed to refill account %d: %v", accountIndex, err)
			atomic.AddInt64(&lt.totalTxsFailed, 1)
			return
		}
		
		// Update local balance after refill
		account.Balance = lt.config.FundAmount
	}
	
	// Get current nonce for account
	nonceResp, err := lt.accountClient.GetCurrentNonce(lt.ctx, &pb.GetCurrentNonceRequest{
		Address: account.PublicKey,
		Tag:     "pending",
	})
	if err != nil {
		log.Printf("Failed to get nonce for account %d: %v", accountIndex, err)
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		return
	}
	
	nextNonce := nonceResp.Nonce + 1
	
	// Choose random recipient (different from sender)
	recipientIndex := (accountIndex + 1) % lt.config.AccountCount
	recipient := &lt.accounts[recipientIndex]
	
	// Create transfer transaction
	txMsg := &pb.TxMsg{
		Type:      0, // Transfer type
		Sender:    account.PublicKey,
		Recipient: recipient.PublicKey,
		Amount:    fmt.Sprintf("%d", lt.config.TransferAmount),
		Timestamp: uint64(time.Now().UnixNano() / int64(time.Millisecond)),
		TextData:  fmt.Sprintf("Transfer from account %d to %d", accountIndex, recipientIndex),
		Nonce:     nextNonce,
		ExtraInfo: "",
	}
	
	// Sign transaction
	signature, err := lt.signTransaction(txMsg, account.PrivateKey)
	if err != nil {
		log.Printf("Failed to sign transaction for account %d: %v", accountIndex, err)
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		return
	}
	
	// Send transaction
	signedTx := &pb.SignedTxMsg{
		TxMsg:     txMsg,
		Signature: signature,
	}
	
	resp, err := lt.client.AddTx(lt.ctx, signedTx)
	if err != nil {
		log.Printf("Failed to send transaction for account %d: %v", accountIndex, err)
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		return
	}
	
	if resp.Ok {
		atomic.AddInt64(&lt.totalTxsSuccess, 1)
		// Update account nonce and balance
		account.Nonce = nextNonce
		account.Balance = currentBalance - lt.config.TransferAmount
	} else {
		atomic.AddInt64(&lt.totalTxsFailed, 1)
		log.Printf("Transaction failed for account %d: %s", accountIndex, resp.Error)
	}
}

func (lt *LoadTester) signTransaction(txMsg *pb.TxMsg, privateKey ed25519.PrivateKey) (string, error) {
	// Serialize transaction for signing (same format as TypeScript)
	data := fmt.Sprintf("%d|%s|%s|%s|%s|%d|%s",
		txMsg.Type,
		txMsg.Sender,
		txMsg.Recipient,
		txMsg.Amount,
		txMsg.TextData,
		txMsg.Nonce,
		txMsg.ExtraInfo,
	)
	
	// Sign with Ed25519
	signature := ed25519.Sign(privateKey, []byte(data))
	
	// Encode signature as base58
	return base58.Encode(signature), nil
}

func (lt *LoadTester) Stop() {
	lt.cancel()
}

func (lt *LoadTester) Close() {
	if lt.conn != nil {
		lt.conn.Close()
	}
}

func (lt *LoadTester) PrintStats() {
	// Calculate test duration (excluding funding time)
	testDuration := time.Since(lt.testStartTime)
	// fundingDuration := lt.testStartTime.Sub(lt.fundingStartTime)
	
	totalTxs := atomic.LoadInt64(&lt.totalTxsSent)
	successTxs := atomic.LoadInt64(&lt.totalTxsSuccess)
	failedTxs := atomic.LoadInt64(&lt.totalTxsFailed)
	
	actualRate := float64(totalTxs) / testDuration.Seconds()

	// Peak/sustained removed
	successRate := float64(successTxs) / float64(totalTxs) * 100
	
	fmt.Println("\n=== LOAD TEST STATISTICS ===")
	// fmt.Printf("Funding duration: %v\n", fundingDuration.Round(time.Second))
	fmt.Printf("Test duration: %v\n", testDuration.Round(time.Second))
	fmt.Printf("Total transactions sent: %d\n", totalTxs)
	fmt.Printf("Successful transactions: %d\n", successTxs)
	fmt.Printf("Failed transactions: %d\n", failedTxs)
	fmt.Printf("Actual rate: %.2f tx/s\n", actualRate)
	fmt.Printf("Success rate: %.2f%%\n", successRate)
	fmt.Printf("Accounts used: %d\n", lt.config.AccountCount)
	fmt.Printf("Switch after: %d transactions\n", lt.config.SwitchAfterTx)
	fmt.Println("=============================")
}