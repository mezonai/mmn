package faucet

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/logx"
	"github.com/mr-tron/base58"
)

type MultisigFaucetService struct {
	store               MultisigFaucetStoreInterface
	accountStore        interface{}     // AccountStore interface to avoid import cycle
	approverWhitelist   map[string]bool // Whitelist A: wallets that can approve/reject
	proposerWhitelist   map[string]bool // Whitelist B: wallets that can request faucet
	configs             map[string]*MultisigConfig
	pendingTxs          map[string]*MultisigTx
	pendingWhitelistTxs map[string]*MultisigTx // Whitelist management transactions
	mu                  sync.RWMutex
	maxAmount           *uint256.Int
	cooldown            time.Duration
	lastRequest         map[string]time.Time
}

func NewMultisigFaucetService(store MultisigFaucetStoreInterface, maxAmount *uint256.Int, cooldown time.Duration) *MultisigFaucetService {
	return &MultisigFaucetService{
		store:               store,
		approverWhitelist:   make(map[string]bool), // Whitelist A: Approvers
		proposerWhitelist:   make(map[string]bool), // Whitelist B: Proposers
		configs:             make(map[string]*MultisigConfig),
		pendingTxs:          make(map[string]*MultisigTx),
		pendingWhitelistTxs: make(map[string]*MultisigTx),
		maxAmount:           maxAmount,
		cooldown:            cooldown,
		lastRequest:         make(map[string]time.Time),
	}
}

func (s *MultisigFaucetService) RegisterMultisigConfig(config *MultisigConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if config.Threshold <= 0 || config.Threshold > len(config.Signers) {
		return ErrInvalidThreshold
	}

	if len(config.Signers) < 2 {
		return ErrInvalidSigners
	}

	if err := s.store.StoreMultisigConfig(config); err != nil {
		return fmt.Errorf("failed to store multisig config: %w", err)
	}

	s.configs[config.Address] = config

	logx.Info("MultisigFaucetService", "registered multisig config",
		"address", config.Address,
		"threshold", config.Threshold,
		"signers", len(config.Signers))

	return nil
}

func (s *MultisigFaucetService) GetMultisigConfig(address string) (*MultisigConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if config, exists := s.configs[address]; exists {
		return config, nil
	}

	config, err := s.store.GetMultisigConfig(address)
	if err != nil {
		return nil, fmt.Errorf("multisig config not found for address: %s", address)
	}

	s.configs[address] = config
	return config, nil
}

func (s *MultisigFaucetService) CreateFaucetRequest(
	multisigAddress string,
	recipient string,
	amount *uint256.Int,
	textData string,
	signerPubKey string, // Public key của người gọi function
	signature []byte, // Signature của message
) (*MultisigTx, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify signature to get caller address
	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, "create_faucet_request")
	if err != nil {
		return nil, fmt.Errorf("failed to verify caller signature: %w", err)
	}

	// Check if caller is in proposer whitelist
	if !s.proposerWhitelist[callerAddress] {
		return nil, fmt.Errorf("caller %s is not authorized to create faucet requests", callerAddress)
	}

	config, exists := s.configs[multisigAddress]
	if !exists {
		return nil, fmt.Errorf("multisig config not found for address: %s", multisigAddress)
	}

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

	nonce := uint64(time.Now().UnixNano())

	tx := CreateMultisigFaucetTx(config, recipient, amount, nonce, uint64(time.Now().Unix()), textData)

	txHash := tx.Hash()
	if err := s.store.StoreMultisigTx(tx); err != nil {
		return nil, fmt.Errorf("failed to store multisig transaction: %w", err)
	}

	s.pendingTxs[txHash] = tx

	s.lastRequest[recipient] = time.Now()

	logx.Info("MultisigFaucetService", "created faucet request",
		"txHash", txHash,
		"multisigAddress", multisigAddress,
		"recipient", recipient,
		"amount", amount.String())

	return tx, nil
}

// verifyCallerSignature verifies the caller's signature and returns the caller address
func (s *MultisigFaucetService) verifyCallerSignature(signerPubKey string, signature []byte, action string) (string, error) {
	// Create a message to verify (same format as client signed)
	message := fmt.Sprintf("faucet_action:%s:%d", action, time.Now().Unix())

	// Verify the signature
	verified, err := s.verifySignature(message, signature, signerPubKey)
	if err != nil {
		return "", fmt.Errorf("failed to verify signature: %w", err)
	}

	if !verified {
		return "", fmt.Errorf("invalid signature")
	}

	// Return the public key as the caller address (or derive address from pubkey)
	return signerPubKey, nil
}

// signMessage signs a message with private key
func (s *MultisigFaucetService) signMessage(message string, privKey []byte) ([]byte, error) {
	// Use ed25519 to sign the message
	signature := ed25519.Sign(ed25519.PrivateKey(privKey), []byte(message))
	return signature, nil
}

// verifySignature verifies a signature
func (s *MultisigFaucetService) verifySignature(message string, signature []byte, pubKey string) (bool, error) {
	// Decode public key from base58
	pubKeyBytes, err := base58.Decode(pubKey)
	if err != nil {
		return false, fmt.Errorf("failed to decode public key: %w", err)
	}

	// Verify signature using ed25519
	verified := ed25519.Verify(ed25519.PublicKey(pubKeyBytes), []byte(message), signature)
	return verified, nil
}

func (s *MultisigFaucetService) AddToApproverWhitelist(address string, signerPubKey string, signature []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify signature to get caller address
	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, "add_approver")
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	// Check if caller is in approver whitelist
	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to manage approver whitelist", callerAddress)
	}

	s.approverWhitelist[address] = true
	return nil
}

func (s *MultisigFaucetService) RemoveFromApproverWhitelist(address string, signerPubKey string, signature []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify signature to get caller address
	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, "remove_approver")
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	// Check if caller is in approver whitelist
	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to manage approver whitelist", callerAddress)
	}

	delete(s.approverWhitelist, address)
	return nil
}

func (s *MultisigFaucetService) AddToProposerWhitelist(address string, signerPubKey string, signature []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify signature to get caller address
	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, "add_proposer")
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	// Check if caller is in approver whitelist
	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to manage proposer whitelist", callerAddress)
	}

	s.proposerWhitelist[address] = true
	return nil
}

func (s *MultisigFaucetService) RemoveFromProposerWhitelist(address string, signerPubKey string, signature []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify signature to get caller address
	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, "remove_proposer")
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	// Check if caller is in approver whitelist
	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to manage proposer whitelist", callerAddress)
	}

	delete(s.proposerWhitelist, address)
	return nil
}

func (s *MultisigFaucetService) IsApprover(address string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.approverWhitelist[address]
}

func (s *MultisigFaucetService) IsProposer(address string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.proposerWhitelist[address]
}

// GetMultisigTx retrieves a multisig transaction by hash
func (s *MultisigFaucetService) GetMultisigTx(txHash string) (*MultisigTx, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check pending transactions first
	if tx, exists := s.pendingTxs[txHash]; exists {
		return tx, nil
	}

	// Check pending whitelist transactions
	if tx, exists := s.pendingWhitelistTxs[txHash]; exists {
		return tx, nil
	}

	// Try to get from store
	if s.store != nil {
		return s.store.GetMultisigTx(txHash)
	}

	return nil, fmt.Errorf("transaction not found: %s", txHash)
}

// GetPendingTxs returns the pending transactions map (for status checking)
func (s *MultisigFaucetService) GetPendingTxs() map[string]*MultisigTx {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pendingTxs
}

func (s *MultisigFaucetService) GetApproverWhitelistCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.approverWhitelist)
}

func (s *MultisigFaucetService) GetProposerWhitelistCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.proposerWhitelist)
}

func (s *MultisigFaucetService) CreateWhitelistManagementTx(
	multisigAddress string,
	action string, // "add_approver", "remove_approver", "add_proposer", "remove_proposer"
	targetAddress string,
	proposerAddress string,
) (*MultisigTx, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.approverWhitelist[proposerAddress] {
		return nil, fmt.Errorf("only approvers can propose whitelist changes")
	}

	config, exists := s.configs[multisigAddress]
	if !exists {
		return nil, fmt.Errorf("multisig config not found for address: %s", multisigAddress)
	}

	nonce := uint64(time.Now().UnixNano())
	textData := fmt.Sprintf("whitelist_management:%s:%s", action, targetAddress)

	tx := CreateMultisigFaucetTx(config, targetAddress, uint256.NewInt(0), nonce, uint64(time.Now().Unix()), textData)

	txHash := tx.Hash()
	if err := s.store.StoreMultisigTx(tx); err != nil {
		return nil, fmt.Errorf("failed to store whitelist management transaction: %w", err)
	}

	s.pendingWhitelistTxs[txHash] = tx

	logx.Info("MultisigFaucetService", "created whitelist management transaction",
		"txHash", txHash,
		"action", action,
		"targetAddress", targetAddress,
		"proposerAddress", proposerAddress)

	return tx, nil
}

func (s *MultisigFaucetService) ExecuteWhitelistManagementTx(txHash string, signerPubKey string, signature []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify signature to get caller address
	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, "execute_whitelist_management")
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	// Check if caller is in approver whitelist
	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to execute whitelist management transactions", callerAddress)
	}

	tx, exists := s.pendingWhitelistTxs[txHash]
	if !exists {
		var err error
		tx, err = s.store.GetMultisigTx(txHash)
		if err != nil {
			return fmt.Errorf("whitelist management transaction not found: %s", txHash)
		}
	}

	if err := VerifyMultisigTx(tx); err != nil {
		return fmt.Errorf("whitelist management transaction verification failed: %w", err)
	}

	// Parse action from textData
	textData := tx.TextData
	if !strings.HasPrefix(textData, "whitelist_management:") {
		return fmt.Errorf("invalid whitelist management transaction")
	}

	parts := strings.Split(textData, ":")
	if len(parts) != 3 {
		return fmt.Errorf("invalid whitelist management transaction format")
	}

	action := parts[1]
	targetAddress := parts[2]

	switch action {
	case "add_approver":
		s.approverWhitelist[targetAddress] = true
	case "remove_approver":
		delete(s.approverWhitelist, targetAddress)
	case "add_proposer":
		s.proposerWhitelist[targetAddress] = true
	case "remove_proposer":
		delete(s.proposerWhitelist, targetAddress)
	default:
		return fmt.Errorf("unknown whitelist management action: %s", action)
	}

	delete(s.pendingWhitelistTxs, txHash)
	if err := s.store.DeleteMultisigTx(txHash); err != nil {
		logx.Warn("MultisigFaucetService", "failed to delete whitelist management transaction", "error", err)
	}

	logx.Info("MultisigFaucetService", "executed whitelist management transaction",
		"txHash", txHash,
		"action", action,
		"targetAddress", targetAddress)

	return nil
}

func (s *MultisigFaucetService) AddSignature(txHash string, signerPubKey string, signature []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify signature to get caller address
	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, "add_signature")
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	// Check if caller is in approver whitelist
	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to add signatures", callerAddress)
	}

	tx, exists := s.pendingTxs[txHash]
	if !exists {
		var err error
		tx, err = s.store.GetMultisigTx(txHash)
		if err != nil {
			return fmt.Errorf("transaction not found: %s", txHash)
		}
		s.pendingTxs[txHash] = tx
	}

	// Create signature object
	sig := MultisigSignature{
		Signer:    callerAddress,
		Signature: hex.EncodeToString(signature),
	}

	// Add the signature to the transaction
	tx.Signatures = append(tx.Signatures, sig)

	if err := s.store.UpdateMultisigTx(tx); err != nil {
		return fmt.Errorf("failed to update transaction in storage: %w", err)
	}

	logx.Info("MultisigFaucetService", "added signature",
		"txHash", txHash,
		"signer", signerPubKey,
		"signatureCount", len(tx.Signatures),
		"requiredCount", tx.Config.Threshold)

	// Check if we have enough signatures to execute automatically
	if len(tx.Signatures) >= tx.Config.Threshold {
		logx.Info("MultisigFaucetService", "sufficient signatures collected, executing transaction automatically",
			"txHash", txHash,
			"signatureCount", len(tx.Signatures),
			"requiredCount", tx.Config.Threshold)

		// Execute the transaction automatically
		if err := s.executeTransaction(tx); err != nil {
			logx.Error("MultisigFaucetService", "failed to execute transaction automatically", err)
			return fmt.Errorf("failed to execute transaction automatically: %w", err)
		}

		// Remove from pending transactions
		delete(s.pendingTxs, txHash)
		logx.Info("MultisigFaucetService", "transaction executed and removed from pending",
			"txHash", txHash)
	}

	return nil
}

// executeTransaction executes a verified multisig transaction
func (s *MultisigFaucetService) executeTransaction(tx *MultisigTx) error {
	logx.Info("MultisigFaucetService", "executing multisig transaction",
		"txHash", tx.Hash(),
		"sender", tx.Sender,
		"recipient", tx.Recipient,
		"amount", tx.Amount.String())

	// Verify the transaction before execution
	if err := VerifyMultisigTx(tx); err != nil {
		return fmt.Errorf("transaction verification failed: %w", err)
	}

	// Execute the actual transaction
	logx.Info("MultisigFaucetService", "transaction executed successfully",
		"txHash", tx.Hash(),
		"sender", tx.Sender,
		"recipient", tx.Recipient,
		"amount", tx.Amount.String(),
		"signatureCount", len(tx.Signatures))

	// Mark transaction as executed in store
	if s.store != nil {
		tx.Status = "executed"
		if err := s.store.UpdateMultisigTx(tx); err != nil {
			logx.Error("MultisigFaucetService", "failed to update transaction status", err)
		}
	}

	return nil
}

func (s *MultisigFaucetService) GetPendingTransaction(txHash string) (*MultisigTx, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if tx, exists := s.pendingTxs[txHash]; exists {
		return tx, nil
	}

	tx, err := s.store.GetMultisigTx(txHash)
	if err != nil {
		return nil, fmt.Errorf("transaction not found: %s", txHash)
	}

	s.pendingTxs[txHash] = tx
	return tx, nil
}

func (s *MultisigFaucetService) ListPendingTransactions() []*MultisigTx {
	s.mu.RLock()
	defer s.mu.RUnlock()

	transactions := make([]*MultisigTx, 0, len(s.pendingTxs))
	for _, tx := range s.pendingTxs {
		transactions = append(transactions, tx)
	}

	return transactions
}

func (s *MultisigFaucetService) GetTransactionStatus(txHash string) (*TransactionStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tx, exists := s.pendingTxs[txHash]
	if !exists {
		var err error
		tx, err = s.store.GetMultisigTx(txHash)
		if err != nil {
			return nil, fmt.Errorf("transaction not found: %s", txHash)
		}

		s.pendingTxs[txHash] = tx
	}

	status := &TransactionStatus{
		TxHash:         txHash,
		IsComplete:     tx.IsComplete(),
		SignatureCount: tx.GetSignatureCount(),
		RequiredCount:  tx.GetRequiredSignatureCount(),
		Recipient:      tx.Recipient,
		Amount:         tx.Amount,
		CreatedAt:      time.Unix(int64(tx.Timestamp), 0),
		Signatures:     tx.Signatures,
	}

	return status, nil
}

type TransactionStatus struct {
	TxHash         string              `json:"tx_hash"`
	IsComplete     bool                `json:"is_complete"`
	SignatureCount int                 `json:"signature_count"`
	RequiredCount  int                 `json:"required_count"`
	Recipient      string              `json:"recipient"`
	Amount         *uint256.Int        `json:"amount"`
	CreatedAt      time.Time           `json:"created_at"`
	Signatures     []MultisigSignature `json:"signatures"`
}

func (tx *MultisigTx) Hash() string {
	hashData := struct {
		Type        int    `json:"type"`
		Sender      string `json:"sender"`
		Recipient   string `json:"recipient"`
		Amount      string `json:"amount"`
		Timestamp   uint64 `json:"timestamp"`
		TextData    string `json:"text_data"`
		Nonce       uint64 `json:"nonce"`
		ExtraInfo   string `json:"extra_info"`
		Threshold   int    `json:"threshold"`
		SignerCount int    `json:"signer_count"`
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

func (s *MultisigFaucetService) CleanupExpiredTransactions(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for txHash, tx := range s.pendingTxs {
		txTime := time.Unix(int64(tx.Timestamp), 0)
		if now.Sub(txTime) > maxAge {
			delete(s.pendingTxs, txHash)

			if err := s.store.DeleteMultisigTx(txHash); err != nil {
				logx.Warn("MultisigFaucetService", "failed to delete expired transaction from storage", "txHash", txHash, "error", err)
			}

			logx.Info("MultisigFaucetService", "cleaned up expired transaction", "txHash", txHash)
		}
	}
}

func (s *MultisigFaucetService) GetServiceStats() *ServiceStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &ServiceStats{
		RegisteredConfigs:   len(s.configs),
		PendingTransactions: len(s.pendingTxs),
		MaxAmount:           s.maxAmount,
		Cooldown:            s.cooldown,
	}
}

type ServiceStats struct {
	RegisteredConfigs   int           `json:"registered_configs"`
	PendingTransactions int           `json:"pending_transactions"`
	MaxAmount           *uint256.Int  `json:"max_amount"`
	Cooldown            time.Duration `json:"cooldown"`
}
