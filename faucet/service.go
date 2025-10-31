package faucet

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/holiman/uint256"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mezonai/mmn/common"
	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/types"
	"github.com/mezonai/mmn/zkverify"
	"github.com/mr-tron/base58"
)

type MultisigFaucetService struct {
	ID                      peer.ID
	store                   store.MultisigFaucetStore
	accountStore            store.AccountStore
	mempool                 *mempool.Mempool
	zkVerify                *zkverify.ZkVerify
	eventRouter             *events.EventRouter
	approverWhitelist       map[string]bool
	proposerWhitelist       map[string]bool
	configs                 map[string]*types.MultisigConfig
	pendingTxs              map[string]*types.MultisigTx
	pendingWhitelistTxs     map[string]*types.MultisigTx
	pendingApproverRequests map[string]*types.WhitelistManagementRequest
	pendingProposerRequests map[string]*types.WhitelistManagementRequest
	pendingVotes            map[string]string
	publishMultisigTx       func([]byte) error
	publishConfig           func([]byte) error
	publishWhitelist        func([]byte) error
	publicRequestFaucetVote func([]byte) error
	maxAmount               *uint256.Int
	lastRequest             map[string]time.Time
	voteCollector           *FaucetVoteCollector
	selfPubKey              string
	privKey                 ed25519.PrivateKey
}

func NewMultisigFaucetService(ID peer.ID, voteThreshold int, store store.MultisigFaucetStore, accountStore store.AccountStore, mempool *mempool.Mempool, maxAmount *uint256.Int, zkVerify *zkverify.ZkVerify, publishMultisigTx, publishConfig, publishWhitelist func([]byte) error, publicRequestFaucetVote func([]byte) error, selfPubKey string, privKey ed25519.PrivateKey, eventRouter *events.EventRouter) *MultisigFaucetService {

	service := &MultisigFaucetService{
		ID:                      ID,
		store:                   store,
		accountStore:            accountStore,
		mempool:                 mempool,
		zkVerify:                zkVerify,
		approverWhitelist:       make(map[string]bool),
		proposerWhitelist:       make(map[string]bool),
		configs:                 make(map[string]*types.MultisigConfig),
		pendingTxs:              make(map[string]*types.MultisigTx),
		pendingWhitelistTxs:     make(map[string]*types.MultisigTx),
		pendingApproverRequests: make(map[string]*types.WhitelistManagementRequest),
		pendingProposerRequests: make(map[string]*types.WhitelistManagementRequest),
		maxAmount:               maxAmount,
		lastRequest:             make(map[string]time.Time),
		publishMultisigTx:       publishMultisigTx,
		publishConfig:           publishConfig,
		publishWhitelist:        publishWhitelist,
		publicRequestFaucetVote: publicRequestFaucetVote,
		voteCollector:           NewFaucetVoteCollector(voteThreshold),
		selfPubKey:              selfPubKey,
		privKey:                 privKey,
		pendingVotes:            make(map[string]string),
		eventRouter:             eventRouter,
	}

	// Load whitelists from database
	service.loadWhitelistsFromDB()

	return service
}

func (s *MultisigFaucetService) broadcastMultisigTx(tx *types.MultisigTx, action string) {
	if s.publishMultisigTx == nil {
		return
	}

	msg := CreateMultisigTxMessage(tx, action)

	data, err := msg.ToJSON()
	if err != nil {
		logx.Error("MultisigFaucetService", "Failed to marshal multisig tx message", err)
		return
	}

	if err := s.publishMultisigTx(data); err != nil {
		logx.Error("MultisigFaucetService", "Failed to publish multisig tx", err)
		return
	}

	if s.eventRouter != nil {
		s.eventRouter.PublishTransactionEvent(events.NewFaucetMultisigTxBroadcastedEvent(tx, action))
	}
}

func (s *MultisigFaucetService) broadcastWhitelist(address, whitelistType, action string) {
	if s.publishWhitelist == nil {
		return
	}

	msg := CreateWhitelistMessage(address, whitelistType, action)

	data, err := msg.ToJSON()
	if err != nil {
		logx.Error("MultisigFaucetService", "Failed to marshal whitelist message", err)
		return
	}

	if err := s.publishWhitelist(data); err != nil {
		logx.Error("MultisigFaucetService", "Failed to publish whitelist", err)
		return
	}

	if s.eventRouter != nil {
		s.eventRouter.PublishTransactionEvent(events.NewFaucetWhitelistBroadcastedEvent(address, whitelistType, action))
	}
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
	if err := s.store.StoreMultisigConfig(config); err != nil {
		return fmt.Errorf("failed to store multisig config: %w", err)
	}

	s.configs[config.Address] = config

	for _, signer := range config.Signers {
		s.approverWhitelist[signer] = true
	}

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
	zkProof string,
	zkPub string,
) (*types.MultisigTx, error) {
	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, CREATE_FAUCET, zkProof, zkPub)
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

	s.broadcastMultisigTx(tx, MultisigTxCreated)

	return tx, nil
}

func (s *MultisigFaucetService) verifyCallerSignature(signerPubKey string, signature []byte, action string, zkProof string, zkPub string) (string, error) {
	message := fmt.Sprintf("%s:%s", FAUCET_ACTION, action)
	sigHex := hex.EncodeToString(signature)
	verified := VerifySignature(message, sigHex, signerPubKey, s.zkVerify, zkProof, zkPub)
	if !verified {
		return "", fmt.Errorf("invalid signature")
	}

	return signerPubKey, nil
}

func (s *MultisigFaucetService) AddToApproverWhitelist(address string, signerPubKey string, signature []byte, zkProof string, zkPub string, approve bool) error {
	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, ADD_APPROVER, zkProof, zkPub)
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to manage approver whitelist", callerAddress)
	}

	if s.approverWhitelist[address] {
		return fmt.Errorf("address %s is already in approver whitelist", address)
	}

	requestKey := fmt.Sprintf("%s:%s", ADD_APPROVER, address)

	request, exists := s.pendingApproverRequests[requestKey]
	if !exists {
		request = &types.WhitelistManagementRequest{
			Action:     ADD_APPROVER,
			TargetAddr: address,
			Signatures: make(map[string]bool),
			CreatedAt:  time.Now(),
		}
		s.pendingApproverRequests[requestKey] = request
	}

	// Check if caller already signed this request
	if _, alreadySigned := request.Signatures[callerAddress]; alreadySigned {
		return fmt.Errorf("signature from %s already exists for this request", callerAddress)
	}

	request.Signatures[callerAddress] = approve

	threshold := s.CalculateThreshold()

	isApprove := IsApprove(request.Signatures, threshold)

	if isApprove {

		s.approverWhitelist[address] = true
		delete(s.pendingApproverRequests, requestKey)

		s.storeApproverWhitelist()

		s.broadcastWhitelist(address, "approver", "add")
		return nil
	}

	isReject := IsReject(request.Signatures, threshold)

	if isReject {
		delete(s.pendingApproverRequests, requestKey)
	}

	return nil
}

func (s *MultisigFaucetService) RemoveFromApproverWhitelist(address string, signerPubKey string, signature []byte, zkProof string, zkPub string, approve bool) error {
	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, REMOVE_APPROVER, zkProof, zkPub)
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to manage approver whitelist", callerAddress)
	}

	if !s.approverWhitelist[address] {
		return fmt.Errorf("address %s is not in approver whitelist", address)
	}

	requestKey := fmt.Sprintf("%s:%s", REMOVE_APPROVER, address)

	request, exists := s.pendingApproverRequests[requestKey]
	if !exists {
		request = &types.WhitelistManagementRequest{
			Action:     REMOVE_APPROVER,
			TargetAddr: address,
			Signatures: make(map[string]bool),
			CreatedAt:  time.Now(),
		}
		s.pendingApproverRequests[requestKey] = request
	}

	if _, alreadySigned := request.Signatures[callerAddress]; alreadySigned {
		return fmt.Errorf("signature from %s already exists for this request", callerAddress)
	}

	request.Signatures[callerAddress] = approve

	threshold := s.CalculateThreshold()

	isApprove := IsApprove(request.Signatures, threshold)

	if isApprove {
		delete(s.approverWhitelist, address)
		delete(s.pendingApproverRequests, requestKey)

		s.storeApproverWhitelist()

		s.broadcastWhitelist(address, "approver", "remove")
		return nil
	}

	isReject := IsReject(request.Signatures, threshold)

	if isReject {
		delete(s.pendingApproverRequests, requestKey)
	}

	return nil
}

func (s *MultisigFaucetService) AddToProposerWhitelist(address string, signerPubKey string, signature []byte, zkProof string, zkPub string, approve bool) error {
	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, ADD_PROPOSER, zkProof, zkPub)
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to manage proposer whitelist", callerAddress)
	}

	if s.proposerWhitelist[address] {
		return fmt.Errorf("address %s is already in proposer whitelist", address)
	}

	requestKey := fmt.Sprintf("%s:%s", ADD_PROPOSER, address)

	request, exists := s.pendingProposerRequests[requestKey]
	if !exists {
		request = &types.WhitelistManagementRequest{
			Action:     ADD_PROPOSER,
			TargetAddr: address,
			Signatures: make(map[string]bool),
			CreatedAt:  time.Now(),
		}
		s.pendingProposerRequests[requestKey] = request
	}

	if _, alreadySigned := request.Signatures[callerAddress]; alreadySigned {
		return fmt.Errorf("signature from %s already exists for this request", callerAddress)
	}

	request.Signatures[callerAddress] = approve

	threshold := s.CalculateThreshold()

	isApprove := IsApprove(request.Signatures, threshold)

	if isApprove {
		s.proposerWhitelist[address] = true
		delete(s.pendingProposerRequests, requestKey)
		s.storeProposerWhitelist()
		s.broadcastWhitelist(address, "proposer", "add")
		return nil
	}

	isReject := IsReject(request.Signatures, threshold)
	if isReject {
		delete(s.pendingProposerRequests, requestKey)
	}

	return nil
}

func (s *MultisigFaucetService) RemoveFromProposerWhitelist(address string, signerPubKey string, signature []byte, zkProof string, zkPub string, approve bool) error {
	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, REMOVE_PROPOSER, zkProof, zkPub)
	if err != nil {
		return fmt.Errorf("failed to verify caller signature: %w", err)
	}

	if !s.approverWhitelist[callerAddress] {
		return fmt.Errorf("caller %s is not authorized to manage proposer whitelist", callerAddress)
	}

	if !s.proposerWhitelist[address] {
		return fmt.Errorf("address %s is not in proposer whitelist", address)
	}

	requestKey := fmt.Sprintf("%s:%s", REMOVE_PROPOSER, address)

	request, exists := s.pendingProposerRequests[requestKey]
	if !exists {
		request = &types.WhitelistManagementRequest{
			Action:     REMOVE_PROPOSER,
			TargetAddr: address,
			Signatures: make(map[string]bool),
			CreatedAt:  time.Now(),
		}
		s.pendingProposerRequests[requestKey] = request
	}

	if _, alreadySigned := request.Signatures[callerAddress]; alreadySigned {
		return fmt.Errorf("signature from %s already exists for this request", callerAddress)
	}

	threshold := s.CalculateThreshold()

	isApprove := IsApprove(request.Signatures, threshold)

	if isApprove {
		delete(s.proposerWhitelist, address)
		delete(s.pendingProposerRequests, requestKey)
		s.storeProposerWhitelist()
		s.broadcastWhitelist(address, "proposer", "remove")
		return nil

	}

	isReject := IsReject(request.Signatures, threshold)

	if isReject {
		delete(s.pendingProposerRequests, requestKey)

	}

	return nil
}

func (s *MultisigFaucetService) storeApproverWhitelist() error {
	approverAddresses := make([]string, 0, len(s.approverWhitelist))
	for addr := range s.approverWhitelist {
		approverAddresses = append(approverAddresses, addr)
	}
	return s.store.StoreApproverWhitelist(approverAddresses)
}

func (s *MultisigFaucetService) storeProposerWhitelist() error {
	proposerAddresses := make([]string, 0, len(s.proposerWhitelist))
	for addr := range s.proposerWhitelist {
		proposerAddresses = append(proposerAddresses, addr)
	}
	return s.store.StoreProposerWhitelist(proposerAddresses)
}

func (s *MultisigFaucetService) IsApprover(address string) bool {
	return s.approverWhitelist[address]
}

func (s *MultisigFaucetService) IsProposer(address string) bool {
	return s.proposerWhitelist[address]
}

func (s *MultisigFaucetService) GetMultisigTx(txHash string) (*types.MultisigTx, error) {
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

func (s *MultisigFaucetService) GetMultisigTxs() []*types.MultisigTx {
	txs, err := s.store.ListMultisigTxs()
	if err != nil {
		logx.Error("MultisigFaucetService", "failed to get multisig txs", err)
		return nil
	}
	return txs
}

func (s *MultisigFaucetService) GetPendingTxs() map[string]*types.MultisigTx {
	return s.pendingTxs
}

func (s *MultisigFaucetService) AddSignature(txHash string, signerPubKey string, signature []byte, zkProof string, zkPub string, approve bool) error {
	callerAddress, err := s.verifyCallerSignature(signerPubKey, signature, ADD_SIGNATURE, zkProof, zkPub)

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

	if tx.Status == STATUS_EXECUTED {
		return fmt.Errorf("transaction has already been executed")
	}

	for _, existingSig := range tx.Signatures {
		if existingSig.Signer == callerAddress {
			return fmt.Errorf("signature from %s already exists", callerAddress)
		}
	}

	sig := types.MultisigSignature{
		Signer:    callerAddress,
		Signature: hex.EncodeToString(signature),
		ZkProof:   zkProof,
		ZkPub:     zkPub,
		Approve:   approve,
	}

	tx.Signatures = append(tx.Signatures, sig)

	if err := s.store.UpdateMultisigTx(tx); err != nil {
		return fmt.Errorf("failed to update transaction in storage: %w", err)
	}

	threshold := s.CalculateThreshold()

	countApprove := 0
	for _, sig := range tx.Signatures {
		if sig.Approve {
			countApprove++
		}
	}

	if countApprove >= threshold {
		s.voteCollector.ProcessVote(txHash, s.ID, true)
		appovers := make([]string, 0, len(s.approverWhitelist))
		for addr := range s.approverWhitelist {
			appovers = append(appovers, addr)
		}

		vote := &RequestFaucetVoteMessage{
			TxHash:   txHash,
			LeaderId: s.selfPubKey,
			Appovers: appovers,
			Proposer: tx.Recipient,
		}
		hash := vote.ComputeHash()
		sig := ed25519.Sign(s.privKey, hash[:])

		vote.Signature = sig

		data, err := vote.ToJSON()
		if err != nil {
			return fmt.Errorf("failed to marshal faucet vote message: %w", err)
		}
		s.publicRequestFaucetVote(data)
		s.pendingVotes[txHash] = txHash
		return nil
	}

	countReject := 0
	for _, sig := range tx.Signatures {
		if !sig.Approve {
			countReject++
		}
	}

	if countReject >= threshold {
		tx.Status = STATUS_REJECTED
		if err := s.store.UpdateMultisigTx(tx); err != nil {
			return fmt.Errorf("failed to update transaction status to rejected: %w", err)
		}
		s.broadcastMultisigTx(tx, MultisigTxRejected)
		return nil

	}

	return nil
}

func (s *MultisigFaucetService) OnFaucetVoteCollected(txHash string, voterID peer.ID, approve bool) {
	s.voteCollector.AddVote(txHash, voterID, approve)
	if s.voteCollector.HasReachedThreshold(txHash) {
		if _, ok := s.pendingVotes[txHash]; ok {
			tx, err := s.GetMultisigTx(txHash)
			if err != nil {
				logx.Error("MultisigFaucetService", "failed to get multisig tx for execution", err)
				return
			}

			if err := s.executeTransaction(tx); err != nil {
				logx.Error("MultisigFaucetService", "failed to execute multisig tx", err)
			}

			delete(s.pendingTxs, txHash)
			delete(s.pendingVotes, txHash)
			s.voteCollector.ClearVotes(txHash)
		}
	}

	if s.voteCollector.HasReachedRejectThreshold(txHash) {
		tx, err := s.GetMultisigTx(txHash)
		if err != nil {
			logx.Error("MultisigFaucetService", "failed to get multisig tx for rejection", err)
			return
		}
		tx.Status = STATUS_REJECTED
		if err := s.store.UpdateMultisigTx(tx); err != nil {
			logx.Error("MultisigFaucetService", "failed to update transaction status to rejected", err)
		}
		s.broadcastMultisigTx(tx, MultisigTxRejected)
	}
}

func (s *MultisigFaucetService) executeTransaction(tx *types.MultisigTx) error {
	message := fmt.Sprintf("%s:%s", FAUCET_ACTION, ADD_SIGNATURE)
	valid := 0
	for _, sig := range tx.Signatures {
		if VerifySignature(message, sig.Signature, sig.Signer, s.zkVerify, sig.ZkProof, sig.ZkPub) {
			valid++
		}
	}

	threshold := s.CalculateThreshold()

	if valid < threshold {
		return fmt.Errorf("transaction verification failed: insufficient signatures: %d < %d", valid, threshold)
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
		logx.Info("MultisigFaucetService", fmt.Sprintf("Faucet transaction added to mempool successfully: tx_hash=%s, recipient=%s, amount=%s, nonce=%d", txHash, faucetTx.Recipient, faucetTx.Amount.String(), faucetTx.Nonce))
	}

	if s.store != nil {
		tx.Status = STATUS_EXECUTED
		if err := s.store.UpdateMultisigTx(tx); err != nil {
			logx.Error("MultisigFaucetService", "failed to update transaction status", err)
		}
	}

	s.broadcastMultisigTx(tx, MultisigTxExecuted)

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
		ExtraInfo: fmt.Sprintf("multisig_tx:%s:signers:%d", tx.Hash(), len(config.Signers)),
	}

	aggregatedSignature := s.aggregateMultisigSignatures(tx)
	faucetTx.Signature = aggregatedSignature

	return faucetTx
}

func (s *MultisigFaucetService) aggregateMultisigSignatures(tx *types.MultisigTx) string {
	var signatures []string
	for _, sig := range tx.Signatures {
		if VerifySignature(fmt.Sprintf("%s:%s", FAUCET_ACTION, ADD_SIGNATURE), sig.Signature, sig.Signer, s.zkVerify, sig.ZkProof, sig.ZkPub) {
			sigBytes, err := hex.DecodeString(sig.Signature)
			if err != nil {
				logx.Error("MultisigFaucetService", "failed to decode signature", err)
				continue
			}

			var userSigJSON []byte
			{
				var incoming UserSig
				if err := jsonx.Unmarshal(sigBytes, &incoming); err == nil && len(incoming.PubKey) > 0 && len(incoming.Sig) > 0 {
					// JSON UserSig
					txUserSig := transaction.UserSig{PubKey: incoming.PubKey, Sig: incoming.Sig}
					userSigJSON, err = jsonx.Marshal(txUserSig)
					if err != nil {
						logx.Error("MultisigFaucetService", "failed to marshal transaction user signature", err)
						continue
					}
				} else {
					// ed25519
					if len(sigBytes) != 64 {
						continue
					}
					pubKeyBytes, err := base58.Decode(sig.Signer)
					if err != nil {
						logx.Error("MultisigFaucetService", "failed to decode signer public key", err)
						continue
					}
					userSig := transaction.UserSig{PubKey: pubKeyBytes, Sig: sigBytes}
					userSigJSON, err = jsonx.Marshal(userSig)
					if err != nil {
						logx.Error("MultisigFaucetService", "failed to marshal user signature", err)
						continue
					}
				}
			}

			signatures = append(signatures, common.EncodeBytesToBase58(userSigJSON))
		}
	}

	return common.EncodeBytesToBase58([]byte(strings.Join(signatures, "|")))
}

func (s *MultisigFaucetService) GetPendingTransaction(txHash string) (*types.MultisigTx, error) {
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

func (s *MultisigFaucetService) GetApproverWhitelist() []string {
	addresses := make([]string, 0, len(s.approverWhitelist))
	for addr := range s.approverWhitelist {
		addresses = append(addresses, addr)
	}
	return addresses
}

func (s *MultisigFaucetService) GetProposerWhitelist() []string {
	addresses := make([]string, 0, len(s.proposerWhitelist))
	for addr := range s.proposerWhitelist {
		addresses = append(addresses, addr)
	}
	return addresses
}

func CreateMultisigFaucetTx(config *types.MultisigConfig, recipient string, amount *uint256.Int, nonce uint64, timestamp uint64, textData string) *types.MultisigTx {
	return &types.MultisigTx{
		Type:       1, // TxTypeFaucet
		Sender:     config.Address,
		Recipient:  recipient,
		Amount:     amount,
		Timestamp:  timestamp,
		TextData:   textData,
		Nonce:      nonce,
		ExtraInfo:  "",
		Signatures: make([]types.MultisigSignature, 0),
		Config:     *config,
	}
}

func (s *MultisigFaucetService) HandleFaucetMultisigTx(msg *FaucetSyncTransactionMessage) error {
	tx := &msg.Data
	switch msg.Type {
	case MultisigTxCreated:
		s.pendingTxs[tx.Hash()] = tx
	case MultisigTxUpdated:
		s.pendingTxs[tx.Hash()] = tx
	case MultisigTxExecuted:
		delete(s.pendingTxs, tx.Hash())
		s.voteCollector.ClearVotes(tx.Hash())
	}
	return s.store.StoreMultisigTx(tx)
}

func (s *MultisigFaucetService) HandleFaucetConfig(msg *FaucetSyncConfigMessage) error {
	config := &msg.Data
	s.configs[config.Address] = config
	return s.store.StoreMultisigConfig(config)
}

func (s *MultisigFaucetService) storeMultisigTxs(txs []*types.MultisigTx) error {
	return s.store.StoreMultisigTxs(txs)
}

func (s *MultisigFaucetService) HandleFaucetWhitelist(msg *FaucetSyncWhitelistMessage) error {
	address := msg.Data.Address
	whitelistType := msg.Data.Type
	switch msg.Type {
	case "add":
		switch whitelistType {
		case "approver":
			s.approverWhitelist[address] = true
			return s.storeApproverWhitelist()
		case "proposer":
			s.proposerWhitelist[address] = true
			return s.storeProposerWhitelist()
		}
	case "remove":
		switch whitelistType {
		case "approver":
			delete(s.approverWhitelist, address)
			return s.storeApproverWhitelist()
		case "proposer":
			delete(s.proposerWhitelist, address)
			return s.storeProposerWhitelist()
		}
	}

	return nil

}

func (s *MultisigFaucetService) VerifyVote(msg *RequestFaucetVoteMessage) bool {
	approvers := msg.Appovers
	if len(approvers) == 0 {
		logx.Warn("MultisigFaucetService", "no approvers in vote", "vote", approvers)
		return false
	}

	for _, approver := range approvers {
		if exists := s.approverWhitelist[approver]; !exists {
			logx.Warn("MultisigFaucetService", "approver whitelist", s.approverWhitelist)
			return false
		}
	}

	proposer := msg.Proposer
	if exists := s.proposerWhitelist[proposer]; !exists {
		logx.Warn("MultisigFaucetService", "proposer whitelist", proposer)
		return false
	}

	return true
}

func (s *MultisigFaucetService) GetFaucetConfig() *RequestInitFaucetConfigMessage {
	return &RequestInitFaucetConfigMessage{
		Approvers:               s.GetApproverWhitelist(),
		Proposers:               s.GetProposerWhitelist(),
		MaxAmount:               s.maxAmount,
		PendingTxs:              s.pendingTxs,
		PendingProposerRequests: s.pendingProposerRequests,
		PendingApproverRequests: s.pendingApproverRequests,
		Configs:                 s.configs,
		Votes:                   s.voteCollector.GetVotes(),
		MultisigTxs:             s.GetMultisigTxs(),
	}
}

func (s *MultisigFaucetService) HandleInitFaucetConfig(msg *RequestInitFaucetConfigMessage) error {
	s.approverWhitelist = make(map[string]bool)
	for _, approver := range msg.Approvers {
		s.approverWhitelist[approver] = true
	}
	s.proposerWhitelist = make(map[string]bool)
	for _, proposer := range msg.Proposers {
		s.proposerWhitelist[proposer] = true
	}
	s.maxAmount = msg.MaxAmount
	s.pendingTxs = msg.PendingTxs
	s.pendingProposerRequests = msg.PendingProposerRequests
	s.pendingApproverRequests = msg.PendingApproverRequests
	s.configs = msg.Configs
	s.voteCollector.setVotes(msg.Votes)
	s.pendingTxs = make(map[string]*types.MultisigTx)
	s.pendingProposerRequests = make(map[string]*types.WhitelistManagementRequest)
	s.pendingApproverRequests = make(map[string]*types.WhitelistManagementRequest)
	for _, request := range msg.PendingProposerRequests {
		s.pendingProposerRequests[request.TargetAddr] = request
	}
	for _, request := range msg.PendingApproverRequests {
		s.pendingApproverRequests[request.TargetAddr] = request
	}
	if err := s.storeMultisigTxs(msg.MultisigTxs); err != nil {
		return fmt.Errorf("failed to store multisig transactions: %w", err)
	}
	return nil
}

func IsApprove(signatures map[string]bool, threshold int) bool {
	count := 0
	for _, approve := range signatures {
		if approve {
			count++
		}
	}
	return count >= threshold
}

func IsReject(signatures map[string]bool, threshold int) bool {
	count := 0
	for _, approve := range signatures {
		if !approve {
			count++
		}
	}

	return count >= threshold
}

func (s *MultisigFaucetService) CalculateThreshold() int {
	totalApprovers := len(s.approverWhitelist)
	vf := totalApprovers / 3
	threshold := max(2*vf, 1)

	return threshold
}
