package faucet

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/logx"
)

type MultisigFaucetService struct {
	store       MultisigFaucetStoreInterface
	configs     map[string]*MultisigConfig
	pendingTxs  map[string]*MultisigTx
	mu          sync.RWMutex
	maxAmount   *uint256.Int
	cooldown    time.Duration
	lastRequest map[string]time.Time
}

func NewMultisigFaucetService(store MultisigFaucetStoreInterface, maxAmount *uint256.Int, cooldown time.Duration) *MultisigFaucetService {
	return &MultisigFaucetService{
		store:       store,
		configs:     make(map[string]*MultisigConfig),
		pendingTxs:  make(map[string]*MultisigTx),
		maxAmount:   maxAmount,
		cooldown:    cooldown,
		lastRequest: make(map[string]time.Time),
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
) (*MultisigTx, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

func (s *MultisigFaucetService) AddSignature(txHash string, signerPubKey string, privKey []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, exists := s.pendingTxs[txHash]
	if !exists {
		var err error
		tx, err = s.store.GetMultisigTx(txHash)
		if err != nil {
			return fmt.Errorf("transaction not found: %s", txHash)
		}
		s.pendingTxs[txHash] = tx
	}

	sig, err := SignMultisigTx(tx, signerPubKey, privKey)
	if err != nil {
		return fmt.Errorf("failed to create signature: %w", err)
	}

	if err := AddSignature(tx, sig); err != nil {
		return fmt.Errorf("failed to add signature: %w", err)
	}

	if err := s.store.UpdateMultisigTx(tx); err != nil {
		return fmt.Errorf("failed to update transaction in storage: %w", err)
	}

	logx.Info("MultisigFaucetService", "added signature",
		"txHash", txHash,
		"signer", signerPubKey,
		"signatureCount", len(tx.Signatures),
		"requiredCount", tx.Config.Threshold)

	return nil
}

func (s *MultisigFaucetService) VerifyAndExecute(txHash string) (*MultisigTx, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, exists := s.pendingTxs[txHash]
	if !exists {
		var err error
		tx, err = s.store.GetMultisigTx(txHash)
		if err != nil {
			return nil, fmt.Errorf("transaction not found: %s", txHash)
		}
	}

	if err := VerifyMultisigTx(tx); err != nil {
		return nil, fmt.Errorf("transaction verification failed: %w", err)
	}

	delete(s.pendingTxs, txHash)
	if err := s.store.DeleteMultisigTx(txHash); err != nil {
		logx.Warn("MultisigFaucetService", "failed to delete transaction from storage", "error", err)
	}

	logx.Info("MultisigFaucetService", "verified and executed transaction",
		"txHash", txHash,
		"signatureCount", len(tx.Signatures),
		"recipient", tx.Recipient,
		"amount", tx.Amount.String())

	return tx, nil
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
