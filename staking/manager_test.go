package staking

import (
	"crypto/ed25519"
	"crypto/rand"
	"math/big"
	"testing"
	"time"

	"github.com/mezonai/mmn/poh"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/p2p"
)

func TestStakeManager_Initialize(t *testing.T) {
	manager := NewStakeManager(nil, nil, nil, nil)
	
	err := manager.Initialize()
	if err != nil {
		t.Errorf("Initialize() unexpected error = %v", err)
	}
	
	if manager.stakePool == nil {
		t.Error("stakePool should not be nil after initialization")
	}
	if manager.scheduler == nil {
		t.Error("scheduler should not be nil after initialization")
	}
	if manager.txProcessor == nil {
		t.Error("txProcessor should not be nil after initialization")
	}
}

func TestStakeManager_ProcessStakeTransaction(t *testing.T) {
	// Create mock dependencies
	pohService := &mockPoHService{}
	ledgerService := &mockLedger{}
	mempoolService := &mockMempool{}
	networkService := &mockNetwork{}
	
	manager := NewStakeManager(pohService, ledgerService, mempoolService, networkService)
	err := manager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize manager: %v", err)
	}

	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	// Create register validator transaction
	tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
	if err != nil {
		t.Fatalf("Failed to create register validator tx: %v", err)
	}

	// Process transaction
	err = manager.ProcessStakeTransaction(tx, pubKey)
	if err != nil {
		t.Errorf("ProcessStakeTransaction() unexpected error = %v", err)
	}

	// Verify validator was registered
	validator, exists := manager.stakePool.GetValidatorInfo(pubKeyStr)
	if !exists {
		t.Error("Validator should be registered")
	}
	if validator.Commission != 5 {
		t.Errorf("Commission = %d, want 5", validator.Commission)
	}
}

func TestStakeManager_StartEpoch(t *testing.T) {
	// Create mock dependencies
	pohService := &mockPoHService{}
	ledgerService := &mockLedger{}
	mempoolService := &mockMempool{}
	networkService := &mockNetwork{}
	
	manager := NewStakeManager(pohService, ledgerService, mempoolService, networkService)
	err := manager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize manager: %v", err)
	}

	// Register some validators first
	validators := make([]string, 3)
	for i := 0; i < 3; i++ {
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate key %d: %v", i, err)
		}
		pubKeyStr := string(pubKey)
		validators[i] = pubKeyStr

		tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
		if err != nil {
			t.Fatalf("Failed to create register validator tx %d: %v", i, err)
		}

		err = manager.ProcessStakeTransaction(tx, pubKey)
		if err != nil {
			t.Fatalf("Failed to process transaction %d: %v", i, err)
		}
	}

	// Test starting new epoch
	epoch := uint64(1)
	err = manager.StartEpoch(epoch)
	if err != nil {
		t.Errorf("StartEpoch() unexpected error = %v", err)
	}

	if manager.currentEpoch != epoch {
		t.Errorf("currentEpoch = %d, want %d", manager.currentEpoch, epoch)
	}

	// Check that leader schedule was generated
	schedule := manager.scheduler.GetCurrentSchedule()
	if len(schedule) == 0 {
		t.Error("Leader schedule should not be empty")
	}

	// Verify equal distribution - each validator should have equal slots
	slotCount := make(map[string]int)
	for _, leader := range schedule {
		slotCount[leader]++
	}

	expectedSlotsPerValidator := len(schedule) / len(validators)
	for validator, count := range slotCount {
		if count < expectedSlotsPerValidator || count > expectedSlotsPerValidator+1 {
			t.Errorf("Validator %s has %d slots, expected around %d", validator, count, expectedSlotsPerValidator)
		}
	}
}

func TestStakeManager_ProcessRewards(t *testing.T) {
	// Create mock dependencies
	pohService := &mockPoHService{}
	ledgerService := &mockLedger{}
	mempoolService := &mockMempool{}
	networkService := &mockNetwork{}
	
	manager := NewStakeManager(pohService, ledgerService, mempoolService, networkService)
	err := manager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize manager: %v", err)
	}

	// Register validators
	validators := make([]string, 2)
	for i := 0; i < 2; i++ {
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate key %d: %v", i, err)
		}
		pubKeyStr := string(pubKey)
		validators[i] = pubKeyStr

		tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 10, privKey, 0) // 10% commission
		if err != nil {
			t.Fatalf("Failed to create register validator tx %d: %v", i, err)
		}

		err = manager.ProcessStakeTransaction(tx, pubKey)
		if err != nil {
			t.Fatalf("Failed to process transaction %d: %v", i, err)
		}
	}

	// Process rewards
	totalRewards := big.NewInt(1000000)
	err = manager.ProcessRewards(totalRewards)
	if err != nil {
		t.Errorf("ProcessRewards() unexpected error = %v", err)
	}

	// Verify rewards were distributed
	for _, validator := range validators {
		info, exists := manager.stakePool.GetValidatorInfo(validator)
		if !exists {
			t.Errorf("Validator %s not found", validator)
			continue
		}
		
		// Each validator should get equal share (since equal stake)
		expectedReward := new(big.Int).Div(totalRewards, big.NewInt(int64(len(validators))))
		if info.AccumulatedRewards.Cmp(big.NewInt(0)) <= 0 {
			t.Errorf("Validator %s should have received rewards", validator)
		}
	}
}

func TestStakeManager_GetStakeDistribution(t *testing.T) {
	// Create mock dependencies
	pohService := &mockPoHService{}
	ledgerService := &mockLedger{}
	mempoolService := &mockMempool{}
	networkService := &mockNetwork{}
	
	manager := NewStakeManager(pohService, ledgerService, mempoolService, networkService)
	err := manager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize manager: %v", err)
	}

	// Register 5 validators with equal stake
	numValidators := 5
	validators := make([]string, numValidators)
	for i := 0; i < numValidators; i++ {
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate key %d: %v", i, err)
		}
		pubKeyStr := string(pubKey)
		validators[i] = pubKeyStr

		tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
		if err != nil {
			t.Fatalf("Failed to create register validator tx %d: %v", i, err)
		}

		err = manager.ProcessStakeTransaction(tx, pubKey)
		if err != nil {
			t.Fatalf("Failed to process transaction %d: %v", i, err)
		}
	}

	// Get stake distribution
	distribution := manager.GetStakeDistribution()
	
	if len(distribution) != numValidators {
		t.Errorf("Distribution length = %d, want %d", len(distribution), numValidators)
	}

	// Verify equal distribution (each should have 1/5 = 0.2)
	expectedWeight := 1.0 / float64(numValidators)
	tolerance := 0.001
	
	for validator, weight := range distribution {
		if weight < expectedWeight-tolerance || weight > expectedWeight+tolerance {
			t.Errorf("Validator %s weight = %f, want %f", validator, weight, expectedWeight)
		}
	}

	// Verify weights sum to 1.0
	var totalWeight float64
	for _, weight := range distribution {
		totalWeight += weight
	}
	if totalWeight < 0.999 || totalWeight > 1.001 {
		t.Errorf("Total weight = %f, want 1.0", totalWeight)
	}
}

func TestStakeManager_GetValidatorList(t *testing.T) {
	// Create mock dependencies
	pohService := &mockPoHService{}
	ledgerService := &mockLedger{}
	mempoolService := &mockMempool{}
	networkService := &mockNetwork{}
	
	manager := NewStakeManager(pohService, ledgerService, mempoolService, networkService)
	err := manager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize manager: %v", err)
	}

	// Initially should be empty
	validators := manager.GetValidatorList()
	if len(validators) != 0 {
		t.Errorf("Initial validator list length = %d, want 0", len(validators))
	}

	// Register some validators
	numValidators := 3
	expectedValidators := make([]string, numValidators)
	for i := 0; i < numValidators; i++ {
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate key %d: %v", i, err)
		}
		pubKeyStr := string(pubKey)
		expectedValidators[i] = pubKeyStr

		tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
		if err != nil {
			t.Fatalf("Failed to create register validator tx %d: %v", i, err)
		}

		err = manager.ProcessStakeTransaction(tx, pubKey)
		if err != nil {
			t.Fatalf("Failed to process transaction %d: %v", i, err)
		}
	}

	// Get validator list
	validators = manager.GetValidatorList()
	if len(validators) != numValidators {
		t.Errorf("Validator list length = %d, want %d", len(validators), numValidators)
	}

	// Verify all expected validators are present
	validatorMap := make(map[string]bool)
	for _, v := range validators {
		validatorMap[v] = true
	}

	for _, expectedValidator := range expectedValidators {
		if !validatorMap[expectedValidator] {
			t.Errorf("Validator %s not found in list", expectedValidator)
		}
	}
}

func TestStakeManager_UpdateEpochRewards(t *testing.T) {
	// Create mock dependencies
	pohService := &mockPoHService{}
	ledgerService := &mockLedger{}
	mempoolService := &mockMempool{}
	networkService := &mockNetwork{}
	
	manager := NewStakeManager(pohService, ledgerService, mempoolService, networkService)
	err := manager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize manager: %v", err)
	}

	// Register validators
	numValidators := 4
	validators := make([]string, numValidators)
	for i := 0; i < numValidators; i++ {
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("Failed to generate key %d: %v", i, err)
		}
		pubKeyStr := string(pubKey)
		validators[i] = pubKeyStr

		tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 8, privKey, 0) // 8% commission
		if err != nil {
			t.Fatalf("Failed to create register validator tx %d: %v", i, err)
		}

		err = manager.ProcessStakeTransaction(tx, pubKey)
		if err != nil {
			t.Fatalf("Failed to process transaction %d: %v", i, err)
		}
	}

	// Update epoch rewards
	rewardData := map[string]*big.Int{
		validators[0]: big.NewInt(1000000), // Different rewards for different validators
		validators[1]: big.NewInt(1500000),
		validators[2]: big.NewInt(800000),
		validators[3]: big.NewInt(1200000),
	}

	err = manager.UpdateEpochRewards(rewardData)
	if err != nil {
		t.Errorf("UpdateEpochRewards() unexpected error = %v", err)
	}

	// Verify rewards were updated correctly
	for validator, expectedReward := range rewardData {
		info, exists := manager.stakePool.GetValidatorInfo(validator)
		if !exists {
			t.Errorf("Validator %s not found", validator)
			continue
		}

		if info.AccumulatedRewards.Cmp(expectedReward) != 0 {
			t.Errorf("Validator %s accumulated rewards = %v, want %v", 
				validator, info.AccumulatedRewards, expectedReward)
		}
	}
}

// Mock implementations for testing
type mockPoHService struct{}

func (m *mockPoHService) GetCurrentTick() uint64                          { return 1000 }
func (m *mockPoHService) IsLeader(tick uint64, validatorPubkey string) bool { return true }
func (m *mockPoHService) GenerateProof(prevHash string) (string, error)  { return "proof", nil }
func (m *mockPoHService) VerifyProof(hash, proof string) bool             { return true }
func (m *mockPoHService) UpdateLeaderSchedule(schedule []string) error   { return nil }

type mockLedger struct{}

func (m *mockLedger) GetBalance(pubkey string) *big.Int          { return big.NewInt(1000000000) }
func (m *mockLedger) UpdateBalance(pubkey string, amount *big.Int) error { return nil }
func (m *mockLedger) RecordReward(validator string, amount *big.Int) error { return nil }

type mockMempool struct{}

func (m *mockMempool) AddStakeTransaction(tx *StakeTransaction) error { return nil }
func (m *mockMempool) GetPendingStakeTransactions() []*StakeTransaction { return nil }
func (m *mockMempool) RemoveStakeTransaction(txHash string) error { return nil }

type mockNetwork struct{}

func (m *mockNetwork) BroadcastStakeTransaction(tx *StakeTransaction) error { return nil }
func (m *mockNetwork) BroadcastEpochUpdate(epoch uint64, schedule []string) error { return nil }

func BenchmarkStakeManager_ProcessStakeTransaction(b *testing.B) {
	// Setup
	pohService := &mockPoHService{}
	ledgerService := &mockLedger{}
	mempoolService := &mockMempool{}
	networkService := &mockNetwork{}
	
	manager := NewStakeManager(pohService, ledgerService, mempoolService, networkService)
	err := manager.Initialize()
	if err != nil {
		b.Fatalf("Failed to initialize manager: %v", err)
	}

	// Generate test transactions
	transactions := make([]*StakeTransaction, b.N)
	pubKeys := make([]ed25519.PublicKey, b.N)
	
	for i := 0; i < b.N; i++ {
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			b.Fatalf("Failed to generate key %d: %v", i, err)
		}
		pubKeys[i] = pubKey
		pubKeyStr := string(pubKey)

		tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, uint64(i))
		if err != nil {
			b.Fatalf("Failed to create transaction %d: %v", i, err)
		}
		transactions[i] = tx
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := manager.ProcessStakeTransaction(transactions[i], pubKeys[i])
		if err != nil {
			b.Fatalf("Failed to process transaction %d: %v", i, err)
		}
	}
}

func BenchmarkStakeManager_StartEpoch(b *testing.B) {
	// Setup
	pohService := &mockPoHService{}
	ledgerService := &mockLedger{}
	mempoolService := &mockMempool{}
	networkService := &mockNetwork{}
	
	manager := NewStakeManager(pohService, ledgerService, mempoolService, networkService)
	err := manager.Initialize()
	if err != nil {
		b.Fatalf("Failed to initialize manager: %v", err)
	}

	// Register some validators
	numValidators := 10
	for i := 0; i < numValidators; i++ {
		pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			b.Fatalf("Failed to generate key %d: %v", i, err)
		}
		pubKeyStr := string(pubKey)

		tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
		if err != nil {
			b.Fatalf("Failed to create register validator tx %d: %v", i, err)
		}

		err = manager.ProcessStakeTransaction(tx, pubKey)
		if err != nil {
			b.Fatalf("Failed to process transaction %d: %v", i, err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := manager.StartEpoch(uint64(i))
		if err != nil {
			b.Fatalf("Failed to start epoch %d: %v", i, err)
		}
	}
}
