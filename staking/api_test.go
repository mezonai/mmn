package staking

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStakeAPI_GetStakeInfo(t *testing.T) {
	// Setup
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)
	api := NewStakeAPI(stakePool)

	// Register some validators first
	pubKey1, privKey1, _ := ed25519.GenerateKey(rand.Reader)
	pubKey2, privKey2, _ := ed25519.GenerateKey(rand.Reader)
	pubKeyStr1 := string(pubKey1)
	pubKeyStr2 := string(pubKey2)

	processor := NewStakeTransactionProcessor(stakePool)

	// Register validator 1
	tx1, _ := CreateRegisterValidatorTx(pubKeyStr1, big.NewInt(10000000), 5, privKey1, 0)
	processor.ProcessTransaction(tx1, pubKey1)

	// Register validator 2
	tx2, _ := CreateRegisterValidatorTx(pubKeyStr2, big.NewInt(15000000), 8, privKey2, 0)
	processor.ProcessTransaction(tx2, pubKey2)

	// Test get all stake info
	req := httptest.NewRequest("GET", "/stake/info", nil)
	w := httptest.NewRecorder()

	api.GetStakeInfo(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var response struct {
		Validators []ValidatorInfo `json:"validators"`
		Total      struct {
			TotalStake   *big.Int `json:"total_stake"`
			NumValidators int     `json:"num_validators"`
		} `json:"total"`
	}

	err := json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(response.Validators) != 2 {
		t.Errorf("Number of validators = %d, want 2", len(response.Validators))
	}
	if response.Total.NumValidators != 2 {
		t.Errorf("Total num validators = %d, want 2", response.Total.NumValidators)
	}

	// Test get specific validator info
	req = httptest.NewRequest("GET", "/stake/info?validator="+pubKeyStr1, nil)
	w = httptest.NewRecorder()

	api.GetStakeInfo(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var singleResponse ValidatorInfo
	err = json.NewDecoder(resp.Body).Decode(&singleResponse)
	if err != nil {
		t.Fatalf("Failed to decode single validator response: %v", err)
	}

	if singleResponse.Pubkey != pubKeyStr1 {
		t.Errorf("Validator pubkey = %v, want %v", singleResponse.Pubkey, pubKeyStr1)
	}
	if singleResponse.Commission != 5 {
		t.Errorf("Commission = %d, want 5", singleResponse.Commission)
	}
}

func TestStakeAPI_GetDistribution(t *testing.T) {
	// Setup
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)
	api := NewStakeAPI(stakePool)

	// Register 3 validators with equal stake
	validators := make([]string, 3)
	processor := NewStakeTransactionProcessor(stakePool)

	for i := 0; i < 3; i++ {
		pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
		pubKeyStr := string(pubKey)
		validators[i] = pubKeyStr

		tx, _ := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
		processor.ProcessTransaction(tx, pubKey)
	}

	// Test get distribution
	req := httptest.NewRequest("GET", "/stake/distribution", nil)
	w := httptest.NewRecorder()

	api.GetDistribution(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var response map[string]float64
	err := json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(response) != 3 {
		t.Errorf("Number of validators in distribution = %d, want 3", len(response))
	}

	// Each validator should have approximately 1/3 = 0.333...
	expectedWeight := 1.0 / 3.0
	tolerance := 0.001

	for validator, weight := range response {
		if weight < expectedWeight-tolerance || weight > expectedWeight+tolerance {
			t.Errorf("Validator %s weight = %f, want %f", validator, weight, expectedWeight)
		}
	}
}

func TestStakeAPI_SubmitStakeTransaction(t *testing.T) {
	// Setup
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)
	api := NewStakeAPI(stakePool)

	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	// Create register validator transaction
	tx, err := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
	if err != nil {
		t.Fatalf("Failed to create transaction: %v", err)
	}

	// Serialize transaction
	txData, err := SerializeTransaction(tx)
	if err != nil {
		t.Fatalf("Failed to serialize transaction: %v", err)
	}

	// Test submit transaction
	req := httptest.NewRequest("POST", "/stake/submit", bytes.NewReader(txData))
	req.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()

	api.SubmitStakeTransaction(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}

	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !response.Success {
		t.Errorf("Expected success, got: %s", response.Message)
	}

	// Verify validator was registered
	validator, exists := stakePool.GetValidatorInfo(pubKeyStr)
	if !exists {
		t.Error("Validator should be registered")
	}
	if validator.Commission != 5 {
		t.Errorf("Commission = %d, want 5", validator.Commission)
	}
}

func TestStakeAPI_GetValidatorList(t *testing.T) {
	// Setup
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)
	api := NewStakeAPI(stakePool)

	// Register some validators
	numValidators := 4
	expectedValidators := make([]string, numValidators)
	processor := NewStakeTransactionProcessor(stakePool)

	for i := 0; i < numValidators; i++ {
		pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
		pubKeyStr := string(pubKey)
		expectedValidators[i] = pubKeyStr

		tx, _ := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
		processor.ProcessTransaction(tx, pubKey)
	}

	// Test get validator list
	req := httptest.NewRequest("GET", "/stake/validators", nil)
	w := httptest.NewRecorder()

	api.GetValidatorList(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var response struct {
		Validators []string `json:"validators"`
		Count      int      `json:"count"`
	}

	err := json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(response.Validators) != numValidators {
		t.Errorf("Number of validators = %d, want %d", len(response.Validators), numValidators)
	}
	if response.Count != numValidators {
		t.Errorf("Count = %d, want %d", response.Count, numValidators)
	}

	// Verify all expected validators are present
	validatorMap := make(map[string]bool)
	for _, v := range response.Validators {
		validatorMap[v] = true
	}

	for _, expectedValidator := range expectedValidators {
		if !validatorMap[expectedValidator] {
			t.Errorf("Validator %s not found in list", expectedValidator)
		}
	}
}

func TestStakeAPI_GetRewards(t *testing.T) {
	// Setup
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)
	api := NewStakeAPI(stakePool)

	// Generate test keys
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	pubKeyStr := string(pubKey)

	processor := NewStakeTransactionProcessor(stakePool)

	// Register validator
	tx, _ := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
	processor.ProcessTransaction(tx, pubKey)

	// Add some rewards
	stakePool.DistributeRewards(map[string]*big.Int{
		pubKeyStr: big.NewInt(1000000),
	})

	// Test get rewards for specific validator
	req := httptest.NewRequest("GET", "/stake/rewards?validator="+pubKeyStr, nil)
	w := httptest.NewRecorder()

	api.GetRewards(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var response struct {
		ValidatorPubkey     string   `json:"validator_pubkey"`
		AccumulatedRewards  *big.Int `json:"accumulated_rewards"`
		LastRewardEpoch     uint64   `json:"last_reward_epoch"`
	}

	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.ValidatorPubkey != pubKeyStr {
		t.Errorf("Validator pubkey = %v, want %v", response.ValidatorPubkey, pubKeyStr)
	}
	if response.AccumulatedRewards.Cmp(big.NewInt(950000)) != 0 { // 1M - 5% commission
		t.Errorf("Accumulated rewards = %v, want %v", response.AccumulatedRewards, big.NewInt(950000))
	}

	// Test get all rewards
	req = httptest.NewRequest("GET", "/stake/rewards", nil)
	w = httptest.NewRecorder()

	api.GetRewards(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var allRewardsResponse map[string]struct {
		AccumulatedRewards  *big.Int `json:"accumulated_rewards"`
		LastRewardEpoch     uint64   `json:"last_reward_epoch"`
	}

	err = json.NewDecoder(resp.Body).Decode(&allRewardsResponse)
	if err != nil {
		t.Fatalf("Failed to decode all rewards response: %v", err)
	}

	if len(allRewardsResponse) != 1 {
		t.Errorf("Number of validators in rewards = %d, want 1", len(allRewardsResponse))
	}

	if reward, exists := allRewardsResponse[pubKeyStr]; exists {
		if reward.AccumulatedRewards.Cmp(big.NewInt(950000)) != 0 {
			t.Errorf("Accumulated rewards = %v, want %v", reward.AccumulatedRewards, big.NewInt(950000))
		}
	} else {
		t.Error("Validator rewards not found")
	}
}

func TestStakeAPI_ErrorHandling(t *testing.T) {
	// Setup
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)
	api := NewStakeAPI(stakePool)

	// Test get info for non-existent validator
	req := httptest.NewRequest("GET", "/stake/info?validator=nonexistent", nil)
	w := httptest.NewRecorder()

	api.GetStakeInfo(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	// Test get rewards for non-existent validator
	req = httptest.NewRequest("GET", "/stake/rewards?validator=nonexistent", nil)
	w = httptest.NewRecorder()

	api.GetRewards(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	// Test submit invalid transaction
	req = httptest.NewRequest("POST", "/stake/submit", bytes.NewReader([]byte("invalid data")))
	req.Header.Set("Content-Type", "application/octet-stream")
	w = httptest.NewRecorder()

	api.SubmitStakeTransaction(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Status code = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestStakeAPI_RegisterRoutes(t *testing.T) {
	// Setup
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)
	api := NewStakeAPI(stakePool)

	// Test route registration (this just ensures no panic occurs)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	// Test that routes are actually registered by making requests
	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	// Test /stake/info endpoint
	resp, err := http.Get(testServer.URL + "/stake/info")
	if err != nil {
		t.Fatalf("Failed to get /stake/info: %v", err)
	}
	resp.Body.Close()

	// Test /stake/distribution endpoint
	resp, err = http.Get(testServer.URL + "/stake/distribution")
	if err != nil {
		t.Fatalf("Failed to get /stake/distribution: %v", err)
	}
	resp.Body.Close()

	// Test /stake/validators endpoint
	resp, err = http.Get(testServer.URL + "/stake/validators")
	if err != nil {
		t.Fatalf("Failed to get /stake/validators: %v", err)
	}
	resp.Body.Close()

	// Test /stake/rewards endpoint
	resp, err = http.Get(testServer.URL + "/stake/rewards")
	if err != nil {
		t.Fatalf("Failed to get /stake/rewards: %v", err)
	}
	resp.Body.Close()
}

func TestStakeAPI_ConcurrentRequests(t *testing.T) {
	// Setup
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)
	api := NewStakeAPI(stakePool)

	// Register some validators first
	processor := NewStakeTransactionProcessor(stakePool)
	for i := 0; i < 5; i++ {
		pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
		pubKeyStr := string(pubKey)

		tx, _ := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
		processor.ProcessTransaction(tx, pubKey)
	}

	// Test concurrent requests
	numRequests := 10
	results := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/stake/info", nil)
			w := httptest.NewRecorder()

			api.GetStakeInfo(w, req)

			if w.Result().StatusCode != http.StatusOK {
				results <- fmt.Errorf("expected status 200, got %d", w.Result().StatusCode)
			} else {
				results <- nil
			}
		}()
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		if err := <-results; err != nil {
			t.Errorf("Concurrent request failed: %v", err)
		}
	}
}

func BenchmarkStakeAPI_GetStakeInfo(b *testing.B) {
	// Setup
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)
	api := NewStakeAPI(stakePool)

	// Register many validators
	processor := NewStakeTransactionProcessor(stakePool)
	for i := 0; i < 100; i++ {
		pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
		pubKeyStr := string(pubKey)

		tx, _ := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
		processor.ProcessTransaction(tx, pubKey)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/stake/info", nil)
		w := httptest.NewRecorder()

		api.GetStakeInfo(w, req)

		if w.Result().StatusCode != http.StatusOK {
			b.Fatalf("Expected status 200, got %d", w.Result().StatusCode)
		}
	}
}

func BenchmarkStakeAPI_GetDistribution(b *testing.B) {
	// Setup
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)
	api := NewStakeAPI(stakePool)

	// Register many validators
	processor := NewStakeTransactionProcessor(stakePool)
	for i := 0; i < 100; i++ {
		pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
		pubKeyStr := string(pubKey)

		tx, _ := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, 0)
		processor.ProcessTransaction(tx, pubKey)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/stake/distribution", nil)
		w := httptest.NewRecorder()

		api.GetDistribution(w, req)

		if w.Result().StatusCode != http.StatusOK {
			b.Fatalf("Expected status 200, got %d", w.Result().StatusCode)
		}
	}
}

func BenchmarkStakeAPI_SubmitTransaction(b *testing.B) {
	// Setup
	stakePool := NewStakePool(big.NewInt(1000000), 10, 8640)
	api := NewStakeAPI(stakePool)

	// Prepare transactions
	transactions := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
		pubKeyStr := string(pubKey)

		tx, _ := CreateRegisterValidatorTx(pubKeyStr, big.NewInt(10000000), 5, privKey, uint64(i))
		txData, _ := SerializeTransaction(tx)
		transactions[i] = txData
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/stake/submit", bytes.NewReader(transactions[i]))
		req.Header.Set("Content-Type", "application/octet-stream")
		w := httptest.NewRecorder()

		api.SubmitStakeTransaction(w, req)

		if w.Result().StatusCode != http.StatusOK {
			b.Fatalf("Expected status 200, got %d", w.Result().StatusCode)
		}
	}
}
