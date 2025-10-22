package faucet

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/logx"
)

// MultisigFaucetService manages multisig faucet operations
type MultisigFaucetService struct {
	configs     map[string]*MultisigConfig // address -> config
	pendingTxs  map[string]*MultisigTx     // tx hash -> pending transaction
	mu          sync.RWMutex
	maxAmount   *uint256.Int
	cooldown    time.Duration
	lastRequest map[string]time.Time // recipient -> last request time
}

// NewMultisigFaucetService creates a new multisig faucet service
func NewMultisigFaucetService(maxAmount *uint256.Int, cooldown time.Duration) *MultisigFaucetService {
	return &MultisigFaucetService{
		configs:     make(map[string]*MultisigConfig),
		pendingTxs:  make(map[string]*MultisigTx),
		maxAmount:   maxAmount,
		cooldown:    cooldown,
		lastRequest: make(map[string]time.Time),
	}
}

// RegisterMultisigConfig registers a new multisig configuration
func (s *MultisigFaucetService) RegisterMultisigConfig(config *MultisigConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate configuration
	if config.Threshold <= 0 || config.Threshold > len(config.Signers) {
		return ErrInvalidThreshold
	}

	if len(config.Signers) < 2 {
		return ErrInvalidSigners
	}

	// Store the configuration
	s.configs[config.Address] = config
	
	logx.Info("MultisigFaucetService", "registered multisig config", 
		"address", config.Address, 
		"threshold", config.Threshold, 
		"signers", len(config.Signers))
	
	return nil
}

// GetMultisigConfig retrieves a multisig configuration by address
func (s *MultisigFaucetService) GetMultisigConfig(address string) (*MultisigConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	config, exists := s.configs[address]
	if !exists {
		return nil, fmt.Errorf("multisig config not found for address: %s", address)
	}

	return config, nil
}

// CreateFaucetRequest creates a new faucet request
func (s *MultisigFaucetService) CreateFaucetRequest(
	multisigAddress string,
	recipient string,
	amount *uint256.Int,
	textData string,
) (*MultisigTx, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get multisig configuration
	config, exists := s.configs[multisigAddress]
	if !exists {
		return nil, fmt.Errorf("multisig config not found for address: %s", multisigAddress)
	}

	// Validate amount
	if amount == nil || amount.IsZero() {
		return nil, fmt.Errorf("amount must be greater than zero")
	}

	if s.maxAmount != nil && amount.Cmp(s.maxAmount) > 0 {
		return nil, fmt.Errorf("amount exceeds maximum allowed: %s", s.maxAmount.String())
	}

	// Check cooldown
	if s.cooldown > 0 {
		if lastTime, exists := s.lastRequest[recipient]; exists {
			if time.Since(lastTime) < s.cooldown {
				return nil, fmt.Errorf("recipient %s must wait %v before next request", recipient, s.cooldown)
			}
		}
	}

	// Generate nonce (using timestamp for simplicity)
	nonce := uint64(time.Now().UnixNano())

	// Create multisig transaction
	tx := CreateMultisigFaucetTx(config, recipient, amount, nonce, uint64(time.Now().Unix()), textData)

	// Store as pending transaction
	txHash := tx.Hash()
	s.pendingTxs[txHash] = tx

	// Update last request time
	s.lastRequest[recipient] = time.Now()

	logx.Info("MultisigFaucetService", "created faucet request",
		"txHash", txHash,
		"multisigAddress", multisigAddress,
		"recipient", recipient,
		"amount", amount.String())

	return tx, nil
}

// AddSignature adds a signature to a pending transaction
func (s *MultisigFaucetService) AddSignature(txHash string, signerPubKey string, privKey []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get pending transaction
	tx, exists := s.pendingTxs[txHash]
	if !exists {
		return fmt.Errorf("transaction not found: %s", txHash)
	}

	// Create signature
	sig, err := SignMultisigTx(tx, signerPubKey, privKey)
	if err != nil {
		return fmt.Errorf("failed to create signature: %w", err)
	}

	// Add signature to transaction
	if err := AddSignature(tx, sig); err != nil {
		return fmt.Errorf("failed to add signature: %w", err)
	}

	logx.Info("MultisigFaucetService", "added signature",
		"txHash", txHash,
		"signer", signerPubKey,
		"signatureCount", len(tx.Signatures),
		"requiredCount", tx.Config.Threshold)

	return nil
}

// VerifyAndExecute verifies a multisig transaction and executes it if valid
func (s *MultisigFaucetService) VerifyAndExecute(txHash string) (*MultisigTx, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get pending transaction
	tx, exists := s.pendingTxs[txHash]
	if !exists {
		return nil, fmt.Errorf("transaction not found: %s", txHash)
	}

	// Verify the transaction
	if err := VerifyMultisigTx(tx); err != nil {
		return nil, fmt.Errorf("transaction verification failed: %w", err)
	}

	// Remove from pending transactions
	delete(s.pendingTxs, txHash)

	logx.Info("MultisigFaucetService", "verified and executed transaction",
		"txHash", txHash,
		"signatureCount", len(tx.Signatures),
		"recipient", tx.Recipient,
		"amount", tx.Amount.String())

	return tx, nil
}

// GetPendingTransaction retrieves a pending transaction
func (s *MultisigFaucetService) GetPendingTransaction(txHash string) (*MultisigTx, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tx, exists := s.pendingTxs[txHash]
	if !exists {
		return nil, fmt.Errorf("transaction not found: %s", txHash)
	}

	return tx, nil
}

// ListPendingTransactions returns all pending transactions
func (s *MultisigFaucetService) ListPendingTransactions() []*MultisigTx {
	s.mu.RLock()
	defer s.mu.RUnlock()

	transactions := make([]*MultisigTx, 0, len(s.pendingTxs))
	for _, tx := range s.pendingTxs {
		transactions = append(transactions, tx)
	}

	return transactions
}

// GetTransactionStatus returns the status of a transaction
func (s *MultisigFaucetService) GetTransactionStatus(txHash string) (*TransactionStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tx, exists := s.pendingTxs[txHash]
	if !exists {
		return nil, fmt.Errorf("transaction not found: %s", txHash)
	}

	status := &TransactionStatus{
		TxHash:           txHash,
		IsComplete:       tx.IsComplete(),
		SignatureCount:   tx.GetSignatureCount(),
		RequiredCount:    tx.GetRequiredSignatureCount(),
		Recipient:        tx.Recipient,
		Amount:           tx.Amount,
		CreatedAt:        time.Unix(int64(tx.Timestamp), 0),
		Signatures:       tx.Signatures,
	}

	return status, nil
}

// TransactionStatus represents the status of a multisig transaction
type TransactionStatus struct {
	TxHash         string                `json:"tx_hash"`
	IsComplete     bool                  `json:"is_complete"`
	SignatureCount int                   `json:"signature_count"`
	RequiredCount  int                   `json:"required_count"`
	Recipient      string                `json:"recipient"`
	Amount         *uint256.Int          `json:"amount"`
	CreatedAt      time.Time             `json:"created_at"`
	Signatures     []MultisigSignature   `json:"signatures"`
}

// Hash returns a hash of the multisig transaction for identification
func (tx *MultisigTx) Hash() string {
	// Create a deterministic hash excluding signatures to avoid circular dependency
	hashData := struct {
		Type      int    `json:"type"`
		Sender    string `json:"sender"`
		Recipient string `json:"recipient"`
		Amount    string `json:"amount"`
		Timestamp uint64 `json:"timestamp"`
		TextData  string `json:"text_data"`
		Nonce     uint64 `json:"nonce"`
		ExtraInfo string `json:"extra_info"`
		Threshold int    `json:"threshold"`
		SignerCount int  `json:"signer_count"`
	}{
		Type:        tx.Type,
		Sender:      tx.Sender,
		Recipient:   tx.Recipient,
		Amount:      tx.Amount.String(),
		Timestamp:   tx.Timestamp,
		TextData:    tx.TextData,
		Nonce:       tx.Nonce,
		ExtraInfo:   tx.ExtraInfo,
		Threshold:   tx.Config.Threshold,
		SignerCount: len(tx.Config.Signers),
	}

	jsonData, _ := json.Marshal(hashData)
	return fmt.Sprintf("%x", jsonData)
}

// CleanupExpiredTransactions removes transactions older than the specified duration
func (s *MultisigFaucetService) CleanupExpiredTransactions(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for txHash, tx := range s.pendingTxs {
		txTime := time.Unix(int64(tx.Timestamp), 0)
		if now.Sub(txTime) > maxAge {
			delete(s.pendingTxs, txHash)
			logx.Info("MultisigFaucetService", "cleaned up expired transaction", "txHash", txHash)
		}
	}
}

// GetServiceStats returns statistics about the service
func (s *MultisigFaucetService) GetServiceStats() *ServiceStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &ServiceStats{
		RegisteredConfigs: len(s.configs),
		PendingTransactions: len(s.pendingTxs),
		MaxAmount: s.maxAmount,
		Cooldown: s.cooldown,
	}
}

// ServiceStats represents statistics about the multisig faucet service
type ServiceStats struct {
	RegisteredConfigs  int           `json:"registered_configs"`
	PendingTransactions int          `json:"pending_transactions"`
	MaxAmount          *uint256.Int  `json:"max_amount"`
	Cooldown           time.Duration `json:"cooldown"`
}
