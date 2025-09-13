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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mr-tron/base58"
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
}

// Account represents a test account
type Account struct {
	PublicKey  string
	PrivateKey ed25519.PrivateKey
	Nonce      uint64
	Balance    uint64
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
	startTime       time.Time
	
	// Faucet account (hardcoded from genesis)
	faucetPrivateKey ed25519.PrivateKey
	faucetPublicKey  string
}

// Faucet private key from genesis (same as in TypeScript tests)
const faucetPrivateKeyHex = "302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee"

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
	flag.IntVar(&config.AccountCount, "accounts", 100, "Number of accounts to create")
	flag.IntVar(&config.TxPerSecond, "rate", 50, "Transactions per second")
	flag.IntVar(&config.SwitchAfterTx, "switch", 10, "Switch account after N transactions")
	flag.Uint64Var(&config.FundAmount, "fund", 10000000000, "Amount to fund each account")
	flag.Uint64Var(&config.TransferAmount, "amount", 100, "Amount per transfer transaction")
	flag.DurationVar(&config.Duration, "duration", 0, "Test duration (0 = run indefinitely)")
	
	flag.Parse()
	
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
		startTime:        time.Now(),
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
		time.Sleep(100 * time.Millisecond)
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
		
		// Wait for transaction to be processed
		time.Sleep(500 * time.Millisecond)
		
		// Verify account was funded by checking balance
		accountResp, err := lt.accountClient.GetAccount(lt.ctx, &pb.GetAccountRequest{
			Address: account.PublicKey,
		})
		if err == nil {
			balance, _ := fmt.Sscanf(accountResp.Balance, "%d", &account.Balance)
			if balance == 1 && account.Balance >= lt.config.FundAmount {
				account.Nonce = 0 // Account starts with nonce 0
				log.Printf("Funded account %d (%s...): %d tokens", 
					accountIndex, account.PublicKey[:8], account.Balance)
				return nil
			}
		}
		
		// If verification failed, retry
		if attempt < maxRetries {
			log.Printf("Funding verification failed for account %d, retrying... (attempt %d/%d)", 
				accountIndex, attempt, maxRetries)
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			continue
		}
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
	log.Printf("Starting load test: %d accounts, %d tx/s, switch after %d txs", 
		lt.config.AccountCount, lt.config.TxPerSecond, lt.config.SwitchAfterTx)
	
	// Calculate interval between transactions
	interval := time.Second / time.Duration(lt.config.TxPerSecond)
	
	// Create ticker for rate limiting
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
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
			log.Println("Test duration completed")
			return nil
		case <-ticker.C:
			// Send transaction
			lt.sendTransaction(currentAccountIndex)
			
			// Update counters
			atomic.AddInt64(&lt.totalTxsSent, 1)
			txCountForCurrentAccount++
			
			// Switch account if needed
			if txCountForCurrentAccount >= lt.config.SwitchAfterTx {
				currentAccountIndex = (currentAccountIndex + 1) % lt.config.AccountCount
				txCountForCurrentAccount = 0
			}
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
	duration := time.Since(lt.startTime)
	totalTxs := atomic.LoadInt64(&lt.totalTxsSent)
	successTxs := atomic.LoadInt64(&lt.totalTxsSuccess)
	failedTxs := atomic.LoadInt64(&lt.totalTxsFailed)
	
	actualRate := float64(totalTxs) / duration.Seconds()
	successRate := float64(successTxs) / float64(totalTxs) * 100
	
	fmt.Println("\n=== LOAD TEST STATISTICS ===")
	fmt.Printf("Duration: %v\n", duration.Round(time.Second))
	fmt.Printf("Total transactions sent: %d\n", totalTxs)
	fmt.Printf("Successful transactions: %d\n", successTxs)
	fmt.Printf("Failed transactions: %d\n", failedTxs)
	fmt.Printf("Actual rate: %.2f tx/s\n", actualRate)
	fmt.Printf("Success rate: %.2f%%\n", successRate)
	fmt.Printf("Accounts used: %d\n", lt.config.AccountCount)
	fmt.Printf("Switch after: %d transactions\n", lt.config.SwitchAfterTx)
	fmt.Println("=============================")
}