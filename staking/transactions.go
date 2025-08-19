package staking

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/mezonai/mmn/logx"
)

// StakeTransaction represents a staking transaction
type StakeTransaction struct {
	Type            StakeTxType `json:"type"`
	ValidatorPubkey string      `json:"validator_pubkey"`
	DelegatorPubkey string      `json:"delegator_pubkey"`
	Amount          *big.Int    `json:"amount"`
	Signature       []byte      `json:"signature"`
	Timestamp       time.Time   `json:"timestamp"`
	Nonce           uint64      `json:"nonce"`
}

// StakeTxType represents the type of staking transaction
type StakeTxType int

const (
	StakeTxTypeRegisterValidator StakeTxType = iota
	StakeTxTypeDelegate
	StakeTxTypeUndelegate
	StakeTxTypeDeactivateValidator
)

// String returns string representation of stake transaction type
func (t StakeTxType) String() string {
	switch t {
	case StakeTxTypeRegisterValidator:
		return "register_validator"
	case StakeTxTypeDelegate:
		return "delegate"
	case StakeTxTypeUndelegate:
		return "undelegate"
	case StakeTxTypeDeactivateValidator:
		return "deactivate_validator"
	default:
		return "unknown"
	}
}

// StakeTransactionProcessor processes staking transactions
type StakeTransactionProcessor struct {
	stakePool *StakePool
	nonces    map[string]uint64 // Track nonces for each account
}

// NewStakeTransactionProcessor creates a new stake transaction processor
func NewStakeTransactionProcessor(stakePool *StakePool) *StakeTransactionProcessor {
	return &StakeTransactionProcessor{
		stakePool: stakePool,
		nonces:    make(map[string]uint64),
	}
}

// CreateRegisterValidatorTx creates a transaction to register a new validator
func CreateRegisterValidatorTx(validatorPubkey string, stakeAmount *big.Int, privKey ed25519.PrivateKey, nonce uint64) (*StakeTransaction, error) {
	tx := &StakeTransaction{
		Type:            StakeTxTypeRegisterValidator,
		ValidatorPubkey: validatorPubkey,
		DelegatorPubkey: validatorPubkey, // Self-delegation
		Amount:          stakeAmount,
		Timestamp:       time.Now(),
		Nonce:           nonce,
	}

	signature, err := signStakeTransaction(tx, privKey)
	if err != nil {
		return nil, err
	}
	tx.Signature = signature

	return tx, nil
}

// CreateDelegateTx creates a delegation transaction
func CreateDelegateTx(delegatorPubkey, validatorPubkey string, amount *big.Int, privKey ed25519.PrivateKey, nonce uint64) (*StakeTransaction, error) {
	tx := &StakeTransaction{
		Type:            StakeTxTypeDelegate,
		ValidatorPubkey: validatorPubkey,
		DelegatorPubkey: delegatorPubkey,
		Amount:          amount,
		Timestamp:       time.Now(),
		Nonce:           nonce,
	}

	signature, err := signStakeTransaction(tx, privKey)
	if err != nil {
		return nil, err
	}
	tx.Signature = signature

	return tx, nil
}

// CreateUndelegateTx creates an undelegation transaction
func CreateUndelegateTx(delegatorPubkey, validatorPubkey string, amount *big.Int, privKey ed25519.PrivateKey, nonce uint64) (*StakeTransaction, error) {
	tx := &StakeTransaction{
		Type:            StakeTxTypeUndelegate,
		ValidatorPubkey: validatorPubkey,
		DelegatorPubkey: delegatorPubkey,
		Amount:          amount,
		Timestamp:       time.Now(),
		Nonce:           nonce,
	}

	signature, err := signStakeTransaction(tx, privKey)
	if err != nil {
		return nil, err
	}
	tx.Signature = signature

	return tx, nil
}

// ProcessTransaction processes a staking transaction
func (stp *StakeTransactionProcessor) ProcessTransaction(tx *StakeTransaction, senderPubKey ed25519.PublicKey) error {
	// Verify transaction signature
	if !stp.verifyStakeTransaction(tx, senderPubKey) {
		return errors.New("invalid transaction signature")
	}

	// Check nonce
	expectedNonce := stp.nonces[tx.DelegatorPubkey]
	if tx.Nonce != expectedNonce {
		return fmt.Errorf("invalid nonce: expected %d, got %d", expectedNonce, tx.Nonce)
	}

	// Process based on transaction type
	var err error
	switch tx.Type {
	case StakeTxTypeRegisterValidator:
		err = stp.processRegisterValidator(tx)
	case StakeTxTypeDelegate:
		err = stp.processDelegate(tx)
	case StakeTxTypeUndelegate:
		err = stp.processUndelegate(tx)
	case StakeTxTypeDeactivateValidator:
		err = stp.processDeactivateValidator(tx)
	default:
		return errors.New("unknown transaction type")
	}

	if err != nil {
		return err
	}

	// Update nonce
	stp.nonces[tx.DelegatorPubkey]++

	logx.Info("STAKING", fmt.Sprintf("Processed %s transaction for %s",
		tx.Type.String(), tx.DelegatorPubkey))

	return nil
}

// Process specific transaction types

func (stp *StakeTransactionProcessor) processRegisterValidator(tx *StakeTransaction) error {
	return stp.stakePool.registerValidator(tx.ValidatorPubkey, tx.Amount, false)
}

func (stp *StakeTransactionProcessor) processDelegate(tx *StakeTransaction) error {
	return stp.stakePool.Delegate(tx.DelegatorPubkey, tx.ValidatorPubkey, tx.Amount)
}

func (stp *StakeTransactionProcessor) processUndelegate(tx *StakeTransaction) error {
	return stp.stakePool.Undelegate(tx.DelegatorPubkey, tx.ValidatorPubkey, tx.Amount)
}

func (stp *StakeTransactionProcessor) processDeactivateValidator(tx *StakeTransaction) error {
	// Implementation for deactivating validator
	validator, exists := stp.stakePool.validators[tx.ValidatorPubkey]
	if !exists {
		return errors.New("validator not found")
	}

	if tx.DelegatorPubkey != tx.ValidatorPubkey {
		return errors.New("only validator can deactivate themselves")
	}

	validator.State = StakeStateDeactivating
	return nil
}

// GetNonce returns the current nonce for an account
func (stp *StakeTransactionProcessor) GetNonce(pubkey string) uint64 {
	return stp.nonces[pubkey]
}

// Signature and verification functions

func signStakeTransaction(tx *StakeTransaction, privKey ed25519.PrivateKey) ([]byte, error) {
	// Create transaction hash without signature
	txData := struct {
		Type            StakeTxType `json:"type"`
		ValidatorPubkey string      `json:"validator_pubkey"`
		DelegatorPubkey string      `json:"delegator_pubkey"`
		Amount          string      `json:"amount"`
		Timestamp       time.Time   `json:"timestamp"`
		Nonce           uint64      `json:"nonce"`
	}{
		Type:            tx.Type,
		ValidatorPubkey: tx.ValidatorPubkey,
		DelegatorPubkey: tx.DelegatorPubkey,
		Amount:          tx.Amount.String(),
		Timestamp:       tx.Timestamp,
		Nonce:           tx.Nonce,
	}

	dataBytes, err := json.Marshal(txData)
	if err != nil {
		return nil, err
	}

	signature := ed25519.Sign(privKey, dataBytes)
	return signature, nil
}

func (stp *StakeTransactionProcessor) verifyStakeTransaction(tx *StakeTransaction, pubKey ed25519.PublicKey) bool {
	// Recreate transaction data
	txData := struct {
		Type            StakeTxType `json:"type"`
		ValidatorPubkey string      `json:"validator_pubkey"`
		DelegatorPubkey string      `json:"delegator_pubkey"`
		Amount          string      `json:"amount"`
		Timestamp       time.Time   `json:"timestamp"`
		Nonce           uint64      `json:"nonce"`
	}{
		Type:            tx.Type,
		ValidatorPubkey: tx.ValidatorPubkey,
		DelegatorPubkey: tx.DelegatorPubkey,
		Amount:          tx.Amount.String(),
		Timestamp:       tx.Timestamp,
		Nonce:           tx.Nonce,
	}

	dataBytes, err := json.Marshal(txData)
	if err != nil {
		return false
	}

	return ed25519.Verify(pubKey, dataBytes, tx.Signature)
}

// SerializeTransaction serializes a stake transaction to bytes
func SerializeTransaction(tx *StakeTransaction) ([]byte, error) {
	return json.Marshal(tx)
}

// DeserializeTransaction deserializes bytes to a stake transaction
func DeserializeTransaction(data []byte) (*StakeTransaction, error) {
	var tx StakeTransaction
	err := json.Unmarshal(data, &tx)
	return &tx, err
}
