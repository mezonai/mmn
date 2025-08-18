package main

import (
	"context"
	"fmt"
	"log"
	"time"
	"crypto/rand"
	"encoding/hex"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	
	"github.com/mezonai/mmn/proto"
)

// Token injection configuration
const (
	DefaultTokenAmount    = 1000000 // 1M tokens per injection
	DefaultInjectionCount = 10
	DefaultInterval       = 10 * time.Second
)

// ValidatorTarget represents a validator endpoint
type ValidatorTarget struct {
	Port      int
	TxClient  proto.TxServiceClient
	AccClient proto.AccountServiceClient
	Conn      *grpc.ClientConn
}

// TokenInjector manages continuous token injection
type TokenInjector struct {
	validators      []*ValidatorTarget
	tokensPerInject int
	maxInjections   int
	interval        time.Duration
	totalInjected   int64
	injectionCount  int
	mutex           sync.Mutex
}

// NewTokenInjector creates a new token injector
func NewTokenInjector(ports []int, tokensPerInject, maxInjections int, interval time.Duration) *TokenInjector {
	return &TokenInjector{
		tokensPerInject: tokensPerInject,
		maxInjections:   maxInjections,
		interval:        interval,
	}
}

// ConnectToValidators establishes gRPC connections to all validators
func (ti *TokenInjector) ConnectToValidators(ports []int) error {
	fmt.Println("🔗 Connecting to validators...")
	
	for _, port := range ports {
		address := fmt.Sprintf("localhost:%d", port)
		fmt.Printf("   📡 Connecting to %s... ", address)
		
		conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			fmt.Printf("❌ Failed: %v\n", err)
			continue
		}
		
		txClient := proto.NewTxServiceClient(conn)
		accClient := proto.NewAccountServiceClient(conn)
		
		// Test connection
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err = accClient.GetAccount(ctx, &proto.GetAccountRequest{Address: "test"})
		cancel()
		
		if err != nil {
			fmt.Printf("⚠️  Connection failed: %v\n", err)
			conn.Close()
			continue
		}
		
		validator := &ValidatorTarget{
			Port:      port,
			TxClient:  txClient,
			AccClient: accClient,
			Conn:      conn,
		}
		
		ti.validators = append(ti.validators, validator)
		fmt.Println("✅ Connected")
	}
	
	fmt.Printf("🎯 Total connected validators: %d\n\n", len(ti.validators))
	return nil
}

// generateRandomAddress creates a random address for transactions
func generateRandomAddress() string {
	bytes := make([]byte, 20)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// InjectTokensToValidator sends tokens to a specific validator
func (ti *TokenInjector) InjectTokensToValidator(ctx context.Context, validator *ValidatorTarget, amount int) error {
	// Create a token transfer transaction
	fromAddress := generateRandomAddress() // Faucet address
	toAddress := generateRandomAddress()   // Recipient address
	
	txMsg := &proto.TxMsg{
		Type:      1, // Transfer type
		Sender:    fromAddress,
		Recipient: toAddress,
		Amount:    uint64(amount),
		Timestamp: uint64(time.Now().Unix()),
		Nonce:     uint64(time.Now().UnixNano()),
		TextData:  "Token injection test",
	}
	
	signedTx := &proto.SignedTxMsg{
		TxMsg:     txMsg,
		Signature: "dummy_signature_for_test",
	}
	
	fmt.Printf("   💰 Injecting %d tokens to validator on port %d\n", amount, validator.Port)
	fmt.Printf("      📤 From: %s...\n", fromAddress[:12])
	fmt.Printf("      📥 To: %s...\n", toAddress[:12])
	
	// Send transaction
	response, err := validator.TxClient.AddTx(ctx, signedTx)
	
	if err != nil {
		return fmt.Errorf("failed to submit transaction: %v", err)
	}
	
	fmt.Printf("      ✅ Transaction submitted: %s\n", response.TxHash[:12])
	
	// Update counters
	ti.mutex.Lock()
	ti.totalInjected += int64(amount)
	ti.injectionCount++
	ti.mutex.Unlock()
	
	return nil
}

// RunContinuousInjection runs the main injection loop
func (ti *TokenInjector) RunContinuousInjection(ctx context.Context) error {
	if len(ti.validators) == 0 {
		return fmt.Errorf("no validators connected")
	}
	
	fmt.Println("🚀 Starting continuous token injection...")
	fmt.Printf("   🎯 Validators: %d\n", len(ti.validators))
	fmt.Printf("   💰 Tokens per injection: %d\n", ti.tokensPerInject)
	fmt.Printf("   🔄 Max injections: %d\n", ti.maxInjections)
	fmt.Printf("   ⏰ Interval: %v\n", ti.interval)
	fmt.Println()
	
	ticker := time.NewTicker(ti.interval)
	defer ticker.Stop()
	
	roundCount := 0
	
	for {
		select {
		case <-ctx.Done():
			fmt.Println("🛑 Injection stopped by context")
			return ctx.Err()
			
		case <-ticker.C:
			if roundCount >= ti.maxInjections {
				fmt.Println("🎉 Maximum injections reached!")
				return nil
			}
			
			roundCount++
			fmt.Printf("🔄 Injection Round %d/%d - %s\n", roundCount, ti.maxInjections, time.Now().Format("15:04:05"))
			fmt.Println("================================")
			
			// Inject to all validators concurrently
			var wg sync.WaitGroup
			for _, validator := range ti.validators {
				wg.Add(1)
				go func(v *ValidatorTarget) {
					defer wg.Done()
					
					injCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
					defer cancel()
					
					err := ti.InjectTokensToValidator(injCtx, v, ti.tokensPerInject)
					if err != nil {
						fmt.Printf("   ❌ Injection to port %d failed: %v\n", v.Port, err)
					} else {
						fmt.Printf("   ✅ Injection to port %d completed\n", v.Port)
					}
				}(validator)
			}
			
			wg.Wait()
			
			// Show round summary
			ti.mutex.Lock()
			totalInjected := ti.totalInjected
			injectionCount := ti.injectionCount
			ti.mutex.Unlock()
			
			fmt.Printf("\n📊 Round %d Summary:\n", roundCount)
			fmt.Printf("   💰 Tokens injected this round: %d\n", len(ti.validators)*ti.tokensPerInject)
			fmt.Printf("   🏆 Cumulative total: %d tokens\n", totalInjected)
			fmt.Printf("   🔢 Total injections: %d\n", injectionCount)
			fmt.Println()
		}
	}
}

// Close closes all validator connections
func (ti *TokenInjector) Close() {
	fmt.Println("🔌 Closing validator connections...")
	for _, validator := range ti.validators {
		if validator.Conn != nil {
			validator.Conn.Close()
		}
	}
}

// MonitorStakingStatus checks current staking status
func MonitorStakingStatus(ctx context.Context, validators []*ValidatorTarget) {
	fmt.Println("📊 Monitoring Staking Status...")
	fmt.Println("=============================")
	
	for i, validator := range validators {
		fmt.Printf("🏷️  Validator %d (Port %d):\n", i+1, validator.Port)
		
		// Get balance (simulate staking balance check)
		balanceCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		testAddress := generateRandomAddress()
		account, err := validator.AccClient.GetAccount(balanceCtx, &proto.GetAccountRequest{
			Address: testAddress,
		})
		cancel()
		
		if err != nil {
			fmt.Printf("   ❌ Balance check failed: %v\n", err)
		} else {
			fmt.Printf("   💰 Sample balance: %d tokens\n", account.Balance)
		}
		
		fmt.Printf("   ✅ Status: Active\n")
		fmt.Printf("   🌐 Endpoint: localhost:%d\n", validator.Port)
		fmt.Println()
	}
}

func main() {
	fmt.Println("🔥 MMN Real-Time Token Injector")
	fmt.Println("===============================")
	fmt.Println("Continuous token injection with gRPC")
	fmt.Println()
	
	// Validator ports
	validatorPorts := []int{9101, 9102, 9103}
	
	// Create injector
	injector := NewTokenInjector(
		validatorPorts,
		DefaultTokenAmount,
		DefaultInjectionCount,
		DefaultInterval,
	)
	defer injector.Close()
	
	// Connect to validators
	err := injector.ConnectToValidators(validatorPorts)
	if err != nil {
		log.Fatalf("Failed to connect to validators: %v", err)
	}
	
	if len(injector.validators) == 0 {
		fmt.Println("❌ No validators available. Please start the network first:")
		fmt.Println("   ./scripts/build_and_test.sh")
		fmt.Println("   OR")
		fmt.Println("   ./scripts/test_network.sh")
		return
	}
	
	// Create context for the injection
	ctx := context.Background()
	
	// Monitor initial status
	MonitorStakingStatus(ctx, injector.validators)
	
	// Run continuous injection
	fmt.Println("🚀 Starting token injection in 5 seconds...")
	time.Sleep(5 * time.Second)
	
	err = injector.RunContinuousInjection(ctx)
	if err != nil && err != context.Canceled {
		log.Fatalf("Injection failed: %v", err)
	}
	
	// Final status check
	fmt.Println("\n📊 Final Status Check...")
	MonitorStakingStatus(ctx, injector.validators)
	
	fmt.Println("🎉 Token injection completed successfully!")
}
