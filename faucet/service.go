package faucet

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/types"
	"github.com/mr-tron/base58"
)

type MultisigFaucetService struct {
	store               MultisigFaucetStoreInterface
	accountStore        store.AccountStore
	mempool             *mempool.Mempool
	approverWhitelist   map[string]bool
	proposerWhitelist   map[string]bool
	configs             map[string]*types.MultisigConfig
	pendingTxs          map[string]*types.MultisigTx
	pendingWhitelistTxs map[string]*types.MultisigTx
	mu                  sync.RWMutex
	maxAmount           *uint256.Int
	cooldown            time.Duration
	lastRequest         map[string]time.Time
}

func NewMultisigFaucetService(store MultisigFaucetStoreInterface, accountStore store.AccountStore, mempool *mempool.Mempool, maxAmount *uint256.Int, cooldown time.Duration) *MultisigFaucetService {
	return &MultisigFaucetService{
		store:               store,
		accountStore:        accountStore,
		mempool:             mempool,
		approverWhitelist:   make(map[string]bool),
		proposerWhitelist:   make(map[string]bool),
		configs:             make(map[string]*types.MultisigConfig),
		pendingTxs:          make(map[string]*types.MultisigTx),
		pendingWhitelistTxs: make(map[string]*types.MultisigTx),
		maxAmount:           maxAmount,
		cooldown:            cooldown,
		lastRequest:         make(map[string]time.Time),
	}
}

func (s *MultisigFaucetService) RegisterMultisigConfig(config *types.MultisigConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if config.Threshold <= 0 || config.Threshold > len(config.Signers) {
		return fmt.Errorf("invalid threshold: %d", config.Threshold)
	}

	if len(config.Signers) < 2 {
		return fmt.Errorf("invalid signers: %d", len(config.Signers))
	}

	if err := s.store.StoreMultisigConfig(config); err != nil {
		return fmt.Errorf("failed to store multisig config: %w", err)
	}

	s.configs[config.Address] = config

	for _, signer := range config.Signers {
		s.approverWhitelist[signer] = true
	}

	return nil
}

func (s *MultisigFaucetService) GetMultisigConfig(address string) (*types.MultisigConfig, error) {
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

func (s *MultisigFaucetService) IsMultisigWallet(address string) bool {
	_, err := s.GetMultisigConfig(address)
	return err == nil
}

func (s *MultisigFaucetService) GetMultisigWalletInfo(address string) (*types.MultisigConfig, error) {
	if !s.IsMultisigWallet(address) {
		return nil, fmt.Errorf("address %s is not a multisig wallet", address)
	}
	return s.GetMultisigConfig(address)
}

func (s *MultisigFaucetService) GetMultisigWalletStats(address string) (map[string]interface{}, error) {
	if !s.IsMultisigWallet(address) {
		return nil, fmt.Errorf("address %s is not a multisig wallet", address)
	}

	config, err := s.GetMultisigConfig(address)
	if err != nil {
		return nil, err
	}

	pendingCount := 0
	s.mu.RLock()
	for _, tx := range s.pendingTxs {
		if tx.Sender == address {
			pendingCount++
		}
	}
	s.mu.RUnlock()

	return map[string]interface{}{
		"address":       address,
		"threshold":     config.Threshold,
		"signers_count": len(config.Signers),
		"signers":       config.Signers,
		"pending_txs":   pendingCount,
		"is_multisig":   true,
	}, nil
}

func (s *MultisigFaucetService) CreateFaucetRequest(
	multisigAddress string,
	amount *uint256.Int,
	textData string,
	signerPubKey string,
	signature []byte,
) (*types.MultisigTx, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, CREATE_FAUCET)
	if err != nil {
		return nil, fmt.Errorf("failed to verify caller signature: %w", err)
	}

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

	if s.cooldown > 0 {
		if lastTime, exists := s.lastRequest[callerAddress]; exists {
			if time.Since(lastTime) < s.cooldown {
				return nil, fmt.Errorf("caller %s must wait %v before next request", callerAddress, s.cooldown)
			}
		}
	}

	nonce := uint64(time.Now().Unix())

	tx := CreateMultisigFaucetTx(config, callerAddress, amount, nonce, uint64(time.Now().Unix()), textData)

	txHash := tx.Hash()
	if err := s.store.StoreMultisigTx(tx); err != nil {
		return nil, fmt.Errorf("failed to store multisig transaction: %w", err)
	}

	s.pendingTxs[txHash] = tx

	s.lastRequest[callerAddress] = time.Now()

	return tx, nil
}

func (s *MultisigFaucetService) verifyCallerSignature(signerPubKey string, signature []byte, action string) (string, error) {
	message := fmt.Sprintf("%s:%s", FAUCET_ACTION, action)

	verified, err := s.verifySignature(message, signature, signerPubKey)
	if err != nil {
		return "", fmt.Errorf("failed to verify signature: %w", err)
	}

	if !verified {
		return "", fmt.Errorf("invalid signature")
	}

	return signerPubKey, nil
}

func (s *MultisigFaucetService) verifySignature(message string, signature []byte, pubKey string) (bool, error) {
	pubKeyBytes, err := base58.Decode(pubKey)
	if err != nil {
		return false, fmt.Errorf("failed to decode public key: %w", err)
	}

	verified := ed25519.Verify(ed25519.PublicKey(pubKeyBytes), []byte(message), signature)
	return verified, nil
}

func (s *MultisigFaucetService) AddToApproverWhitelist(address string, signerPubKey string, signature []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, ADD_APPROVER)
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to manage approver whitelist", callerAddress)
	}

	s.approverWhitelist[address] = true
	return nil
}

func (s *MultisigFaucetService) RemoveFromApproverWhitelist(address string, signerPubKey string, signature []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, REMOVE_APPROVER)
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to manage approver whitelist", callerAddress)
	}

	delete(s.approverWhitelist, address)
	return nil
}

func (s *MultisigFaucetService) AddToProposerWhitelist(address string, signerPubKey string, signature []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, ADD_PROPOSER)
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to manage proposer whitelist", callerAddress)
	}

	s.proposerWhitelist[address] = true
	return nil
}

func (s *MultisigFaucetService) RemoveFromProposerWhitelist(address string, signerPubKey string, signature []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, REMOVE_APPROVER)
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

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

func (s *MultisigFaucetService) GetMultisigTx(txHash string) (*types.MultisigTx, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if tx, exists := s.pendingTxs[txHash]; exists {
		return tx, nil
	}

	if tx, exists := s.pendingWhitelistTxs[txHash]; exists {
		return tx, nil
	}

	if s.store != nil {
		return s.store.GetMultisigTx(txHash)
	}

	return nil, fmt.Errorf("transaction not found: %s", txHash)
}

func (s *MultisigFaucetService) GetPendingTxs() map[string]*types.MultisigTx {
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
	action string,
	targetAddress string,
	proposerAddress string,
) (*types.MultisigTx, error) {
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

	return tx, nil
}

func (s *MultisigFaucetService) ExecuteWhitelistManagementTx(txHash string, signerPubKey string, signature []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, EXECUTE_WHITELIST_MANAGEMENT)
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

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

	textData := tx.TextData
	if !strings.HasPrefix(textData, WHITELIST_MANAGEMENT_PREFIX) {
		return fmt.Errorf("invalid whitelist management transaction")
	}

	parts := strings.Split(textData, ":")
	if len(parts) != 3 {
		return fmt.Errorf("invalid whitelist management transaction format")
	}

	action := parts[1]
	targetAddress := parts[2]

	switch action {
	case ADD_APPROVER:
		s.approverWhitelist[targetAddress] = true
	case REMOVE_APPROVER:
		delete(s.approverWhitelist, targetAddress)
	case ADD_PROPOSER:
		s.proposerWhitelist[targetAddress] = true
	case REMOVE_PROPOSER:
		delete(s.proposerWhitelist, targetAddress)
	default:
		return fmt.Errorf("unknown whitelist management action: %s", action)
	}

	delete(s.pendingWhitelistTxs, txHash)
	if err := s.store.DeleteMultisigTx(txHash); err != nil {
		logx.Warn("MultisigFaucetService", "failed to delete whitelist management transaction", "error", err)
	}

	return nil
}

func (s *MultisigFaucetService) AddSignature(txHash string, signerPubKey string, signature []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, ADD_SIGNATURE)
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

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

	for _, existingSig := range tx.Signatures {
		if existingSig.Signer == callerAddress {
			return fmt.Errorf("signature from %s already exists", callerAddress)
		}
	}

	sig := types.MultisigSignature{
		Signer:    callerAddress,
		Signature: hex.EncodeToString(signature),
	}

	tx.Signatures = append(tx.Signatures, sig)

	if err := s.store.UpdateMultisigTx(tx); err != nil {
		return fmt.Errorf("failed to update transaction in storage: %w", err)
	}

	if len(tx.Signatures) >= tx.Config.Threshold {
		if err := s.executeTransaction(tx); err != nil {
			logx.Error("MultisigFaucetService", "failed to execute transaction automatically", err)
		}

		delete(s.pendingTxs, txHash)
	}

	return nil
}

func (s *MultisigFaucetService) executeTransaction(tx *types.MultisigTx) error {

	if err := VerifyMultisigTx(tx); err != nil {
		return fmt.Errorf("transaction verification failed: %w", err)
	}

	faucetAccount, err := s.accountStore.GetByAddr(tx.Sender)
	if err != nil {
		return fmt.Errorf("failed to get faucet account: %w", err)
	}

	if faucetAccount.Balance.Cmp(tx.Amount) < 0 {
		return fmt.Errorf("insufficient faucet balance: %s < %s", faucetAccount.Balance.String(), tx.Amount.String())
	}

	faucetTx := s.createFaucetTransaction(tx)
	if faucetTx == nil {
		return fmt.Errorf("failed to create faucet transaction")
	}

	if s.mempool != nil {
		txHash, err := s.mempool.AddTx(faucetTx, true)
		if err != nil {
			return fmt.Errorf("failed to add transaction to mempool: %w", err)
		}
		logx.Info("MultisigFaucetService", "faucet transaction added to mempool", "txHash", txHash)
	}

	if s.store != nil {
		tx.Status = STATUS_EXECUTED
		if err := s.store.UpdateMultisigTx(tx); err != nil {
			logx.Error("MultisigFaucetService", "failed to update transaction status", err)
		}
	}

	return nil
}

func (s *MultisigFaucetService) createFaucetTransaction(tx *types.MultisigTx) *transaction.Transaction {
	if !s.IsMultisigWallet(tx.Sender) {
		logx.Error("MultisigFaucetService", "sender is not a multisig wallet", "sender", tx.Sender)
		return nil
	}

	faucetAccount, err := s.accountStore.GetByAddr(tx.Sender)
	if err != nil {
		logx.Error("MultisigFaucetService", "failed to get faucet account for nonce", err)
		return nil
	}

	config, err := s.GetMultisigConfig(tx.Sender)
	if err != nil {
		logx.Error("MultisigFaucetService", "failed to get multisig config", err)
		return nil
	}

	faucetTx := &transaction.Transaction{
		Type:      transaction.TxTypeFaucet,
		Sender:    tx.Sender,
		Recipient: tx.Recipient,
		Amount:    tx.Amount,
		Timestamp: uint64(time.Now().UnixNano() / int64(time.Millisecond)),
		TextData:  tx.TextData,
		Nonce:     faucetAccount.Nonce + 1,
		ExtraInfo: fmt.Sprintf("multisig_tx:%s:threshold:%d:signers:%d", tx.Hash(), config.Threshold, len(config.Signers)),
	}

	return faucetTx
}

func (s *MultisigFaucetService) GetPendingTransaction(txHash string) (*types.MultisigTx, error) {
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

func (s *MultisigFaucetService) ListPendingTransactions() []*types.MultisigTx {
	s.mu.RLock()
	defer s.mu.RUnlock()

	transactions := make([]*types.MultisigTx, 0, len(s.pendingTxs))
	for _, tx := range s.pendingTxs {
		transactions = append(transactions, tx)
	}

	return transactions
}

func (s *MultisigFaucetService) GetTransactionStatus(txHash string) (*types.TransactionStatus, error) {
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

	status := &types.TransactionStatus{
		TxHash:         txHash,
		IsComplete:     len(tx.Signatures) >= tx.Config.Threshold,
		SignatureCount: len(tx.Signatures),
		RequiredCount:  tx.Config.Threshold,
		Recipient:      tx.Recipient,
		Amount:         tx.Amount,
		CreatedAt:      time.Unix(int64(tx.Timestamp), 0),
		Signatures:     tx.Signatures,
	}

	return status, nil
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
		}
	}
}

func (s *MultisigFaucetService) GetServiceStats() *types.ServiceStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &types.ServiceStats{
		RegisteredConfigs:   len(s.configs),
		PendingTransactions: len(s.pendingTxs),
		MaxAmount:           s.maxAmount,
		Cooldown:            s.cooldown,
	}
}
