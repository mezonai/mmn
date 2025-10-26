package faucet

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
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

func calculateThreshold(approverCount int) int {
	threshold := (approverCount * thresholdRatio) / thresholdDivisor
	if threshold < minThreshold {
		threshold = minThreshold
	}
	return threshold
}

type MultisigFaucetService struct {
	store                   MultisigFaucetStoreInterface
	accountStore            store.AccountStore
	mempool                 *mempool.Mempool
	approverWhitelist       map[string]bool
	proposerWhitelist       map[string]bool
	configs                 map[string]*types.MultisigConfig
	pendingTxs              map[string]*types.MultisigTx
	pendingWhitelistTxs     map[string]*types.MultisigTx
	pendingApproverRequests map[string]*types.WhitelistManagementRequest
	pendingProposerRequests map[string]*types.WhitelistManagementRequest
	mu                      sync.RWMutex
	maxAmount               *uint256.Int
	cooldown                time.Duration
	lastRequest             map[string]time.Time
}

func NewMultisigFaucetService(store MultisigFaucetStoreInterface, accountStore store.AccountStore, mempool *mempool.Mempool, maxAmount *uint256.Int, cooldown time.Duration) *MultisigFaucetService {
	service := &MultisigFaucetService{
		store:                   store,
		accountStore:            accountStore,
		mempool:                 mempool,
		approverWhitelist:       make(map[string]bool),
		proposerWhitelist:       make(map[string]bool),
		configs:                 make(map[string]*types.MultisigConfig),
		pendingTxs:              make(map[string]*types.MultisigTx),
		pendingWhitelistTxs:     make(map[string]*types.MultisigTx),
		pendingApproverRequests: make(map[string]*types.WhitelistManagementRequest),
		pendingProposerRequests: make(map[string]*types.WhitelistManagementRequest),
		maxAmount:               maxAmount,
		cooldown:                cooldown,
		lastRequest:             make(map[string]time.Time),
	}

	// Load whitelists from database
	service.loadWhitelistsFromDB()

	return service
}

func (s *MultisigFaucetService) loadWhitelistsFromDB() {
	approverAddresses, err := s.store.GetApproverWhitelist()
	if err != nil {
		logx.Warn("MultisigFaucetService", "failed to load approver whitelist", "error", err)
		return
	}

	for _, addr := range approverAddresses {
		s.approverWhitelist[addr] = true
	}

	proposerAddresses, err := s.store.GetProposerWhitelist()
	if err != nil {
		logx.Warn("MultisigFaucetService", "failed to load proposer whitelist", "error", err)
		return
	}

	for _, addr := range proposerAddresses {
		s.proposerWhitelist[addr] = true
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

	// Initialize approver whitelist with signers
	for _, signer := range config.Signers {
		s.approverWhitelist[signer] = true
	}

	// Store initial whitelist to database
	approverAddresses := make([]string, 0, len(s.approverWhitelist))
	for addr := range s.approverWhitelist {
		approverAddresses = append(approverAddresses, addr)
	}
	s.store.StoreApproverWhitelist(approverAddresses)

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

	// Check faucet balance before creating proposal
	faucetAccount, err := s.accountStore.GetByAddr(multisigAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to get faucet account: %w", err)
	}
	if faucetAccount.Balance.Cmp(amount) < 0 {
		return nil, fmt.Errorf("insufficient faucet balance: %s < %s", faucetAccount.Balance.String(), amount.String())
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

	// Check if address is already in whitelist
	if s.approverWhitelist[address] {
		return fmt.Errorf("address %s is already in approver whitelist", address)
	}

	requestKey := fmt.Sprintf("%s:%s", ADD_APPROVER, address)

	request, exists := s.pendingApproverRequests[requestKey]
	if !exists {
		request = &types.WhitelistManagementRequest{
			Action:     ADD_APPROVER,
			TargetAddr: address,
			Signatures: make(map[string]string),
			CreatedAt:  time.Now(),
		}
		s.pendingApproverRequests[requestKey] = request
	}

	// Check if caller already signed this request
	if _, alreadySigned := request.Signatures[callerAddress]; alreadySigned {
		return fmt.Errorf("signature from %s already exists for this request", callerAddress)
	}

	request.Signatures[callerAddress] = hex.EncodeToString(signature)

	configs := s.getAllMultisigConfigs()
	if len(configs) == 0 {
		return fmt.Errorf("no multisig config found")
	}
	config := configs[0]

	if len(request.Signatures) >= config.Threshold {
		s.approverWhitelist[address] = true
		delete(s.pendingApproverRequests, requestKey)

		s.storeApproverWhitelist()

		newThreshold := calculateThreshold(len(s.approverWhitelist))
		if newThreshold != config.Threshold {
			config.Threshold = newThreshold
			s.store.StoreMultisigConfig(config)
		}
	}

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

	// Check if address exists in whitelist
	if !s.approverWhitelist[address] {
		return fmt.Errorf("address %s is not in approver whitelist", address)
	}

	requestKey := fmt.Sprintf("%s:%s", REMOVE_APPROVER, address)

	request, exists := s.pendingApproverRequests[requestKey]
	if !exists {
		request = &types.WhitelistManagementRequest{
			Action:     REMOVE_APPROVER,
			TargetAddr: address,
			Signatures: make(map[string]string),
			CreatedAt:  time.Now(),
		}
		s.pendingApproverRequests[requestKey] = request
	}

	if _, alreadySigned := request.Signatures[callerAddress]; alreadySigned {
		return fmt.Errorf("signature from %s already exists for this request", callerAddress)
	}

	request.Signatures[callerAddress] = hex.EncodeToString(signature)

	configs := s.getAllMultisigConfigs()
	if len(configs) == 0 {
		return fmt.Errorf("no multisig config found")
	}
	config := configs[0]

	if len(request.Signatures) >= config.Threshold {
		delete(s.approverWhitelist, address)
		delete(s.pendingApproverRequests, requestKey)

		s.storeApproverWhitelist()

		newThreshold := calculateThreshold(len(s.approverWhitelist))
		if newThreshold != config.Threshold {
			config.Threshold = newThreshold
			s.store.StoreMultisigConfig(config)
		}
	}

	return nil
}

func (s *MultisigFaucetService) getAllMultisigConfigs() []*types.MultisigConfig {
	var configs []*types.MultisigConfig
	for _, config := range s.configs {
		configs = append(configs, config)
	}
	return configs
}

func (s *MultisigFaucetService) storeApproverWhitelist() {
	approverAddresses := make([]string, 0, len(s.approverWhitelist))
	for addr := range s.approverWhitelist {
		approverAddresses = append(approverAddresses, addr)
	}
	s.store.StoreApproverWhitelist(approverAddresses)
}

func (s *MultisigFaucetService) storeProposerWhitelist() {
	proposerAddresses := make([]string, 0, len(s.proposerWhitelist))
	for addr := range s.proposerWhitelist {
		proposerAddresses = append(proposerAddresses, addr)
	}
	s.store.StoreProposerWhitelist(proposerAddresses)
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

	// Check if address is already in whitelist
	if s.proposerWhitelist[address] {
		return fmt.Errorf("address %s is already in proposer whitelist", address)
	}

	requestKey := fmt.Sprintf("%s:%s", ADD_PROPOSER, address)

	request, exists := s.pendingProposerRequests[requestKey]
	if !exists {
		request = &types.WhitelistManagementRequest{
			Action:     ADD_PROPOSER,
			TargetAddr: address,
			Signatures: make(map[string]string),
			CreatedAt:  time.Now(),
		}
		s.pendingProposerRequests[requestKey] = request
	}

	// Check if caller already signed this request
	if _, alreadySigned := request.Signatures[callerAddress]; alreadySigned {
		return fmt.Errorf("signature from %s already exists for this request", callerAddress)
	}

	request.Signatures[callerAddress] = hex.EncodeToString(signature)

	configs := s.getAllMultisigConfigs()
	if len(configs) == 0 {
		return fmt.Errorf("no multisig config found")
	}
	config := configs[0]

	if len(request.Signatures) >= config.Threshold {
		s.proposerWhitelist[address] = true
		delete(s.pendingProposerRequests, requestKey)

		s.storeProposerWhitelist()
	}

	return nil
}

func (s *MultisigFaucetService) RemoveFromProposerWhitelist(address string, signerPubKey string, signature []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, REMOVE_PROPOSER)
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to manage proposer whitelist", callerAddress)
	}

	// Check if address exists in whitelist
	if !s.proposerWhitelist[address] {
		return fmt.Errorf("address %s is not in proposer whitelist", address)
	}

	requestKey := fmt.Sprintf("%s:%s", REMOVE_PROPOSER, address)

	request, exists := s.pendingProposerRequests[requestKey]
	if !exists {
		request = &types.WhitelistManagementRequest{
			Action:     REMOVE_PROPOSER,
			TargetAddr: address,
			Signatures: make(map[string]string),
			CreatedAt:  time.Now(),
		}
		s.pendingProposerRequests[requestKey] = request
	}

	// Check if caller already signed this request
	if _, alreadySigned := request.Signatures[callerAddress]; alreadySigned {
		return fmt.Errorf("signature from %s already exists for this request", callerAddress)
	}

	request.Signatures[callerAddress] = hex.EncodeToString(signature)

	configs := s.getAllMultisigConfigs()
	if len(configs) == 0 {
		return fmt.Errorf("no multisig config found")
	}
	config := configs[0]

	if len(request.Signatures) >= config.Threshold {
		delete(s.proposerWhitelist, address)
		delete(s.pendingProposerRequests, requestKey)

		s.storeProposerWhitelist()
	}

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

	// Check if transaction is already executed or rejected
	if tx.Status == STATUS_EXECUTED {
		return fmt.Errorf("transaction has already been executed")
	}
	if tx.Status == STATUS_REJECTED {
		return fmt.Errorf("transaction has already been rejected")
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

func (s *MultisigFaucetService) RejectProposal(txHash string, signerPubKey string, signature []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, REJECT_PROPOSAL)
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to reject proposals", callerAddress)
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

	// Check if transaction is already executed or rejected
	if tx.Status == STATUS_EXECUTED {
		return fmt.Errorf("transaction has already been executed")
	}
	if tx.Status == STATUS_REJECTED {
		return fmt.Errorf("transaction has already been rejected")
	}

	// Mark as rejected
	tx.Status = STATUS_REJECTED

	if err := s.store.UpdateMultisigTx(tx); err != nil {
		return fmt.Errorf("failed to update transaction in storage: %w", err)
	}

	// Remove from pending
	delete(s.pendingTxs, txHash)

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
		_, err := s.mempool.AddTx(faucetTx, true)
		if err != nil {
			return fmt.Errorf("failed to add transaction to mempool: %w", err)
		}
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
func (s *MultisigFaucetService) GetApproverWhitelist() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	addresses := make([]string, 0, len(s.approverWhitelist))
	for addr := range s.approverWhitelist {
		addresses = append(addresses, addr)
	}
	return addresses
}

func (s *MultisigFaucetService) GetProposerWhitelist() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	addresses := make([]string, 0, len(s.proposerWhitelist))
	for addr := range s.proposerWhitelist {
		addresses = append(addresses, addr)
	}
	return addresses
}
