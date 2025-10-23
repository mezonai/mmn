package store

import (
	"fmt"

	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/faucet"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
)

// MultisigFaucetStore is the interface for multisig faucet store
type MultisigFaucetStore interface {
	// Multisig Config operations
	StoreMultisigConfig(config *faucet.MultisigConfig) error
	GetMultisigConfig(address string) (*faucet.MultisigConfig, error)
	ListMultisigConfigs() ([]*faucet.MultisigConfig, error)
	DeleteMultisigConfig(address string) error

	// Multisig Transaction operations
	StoreMultisigTx(tx *faucet.MultisigTx) error
	GetMultisigTx(txHash string) (*faucet.MultisigTx, error)
	ListMultisigTxs() ([]*faucet.MultisigTx, error)
	DeleteMultisigTx(txHash string) error
	UpdateMultisigTx(tx *faucet.MultisigTx) error

	// Signature operations
	AddSignature(txHash string, sig *faucet.MultisigSignature) error
	GetSignatures(txHash string) ([]faucet.MultisigSignature, error)

	// Cleanup operations
	CleanupExpiredTxs(maxAge int64) error

	// Close operations
	MustClose()
}

// GenericMultisigFaucetStore provides multisig faucet storage operations
type GenericMultisigFaucetStore struct {
	dbProvider db.DatabaseProvider
}

// NewGenericMultisigFaucetStore creates a new multisig faucet store
func NewGenericMultisigFaucetStore(dbProvider db.DatabaseProvider) (*GenericMultisigFaucetStore, error) {
	if dbProvider == nil {
		return nil, fmt.Errorf("provider cannot be nil")
	}

	return &GenericMultisigFaucetStore{
		dbProvider: dbProvider,
	}, nil
}

// StoreMultisigConfig stores a multisig configuration
func (s *GenericMultisigFaucetStore) StoreMultisigConfig(config *faucet.MultisigConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	configData, err := jsonx.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal multisig config: %w", err)
	}

	key := s.getMultisigConfigKey(config.Address)
	if err := s.dbProvider.Put(key, configData); err != nil {
		return fmt.Errorf("failed to store multisig config: %w", err)
	}

	logx.Info("MULTISIG_STORE", "stored multisig config", "address", config.Address)
	return nil
}

// GetMultisigConfig retrieves a multisig configuration by address
func (s *GenericMultisigFaucetStore) GetMultisigConfig(address string) (*faucet.MultisigConfig, error) {
	key := s.getMultisigConfigKey(address)
	data, err := s.dbProvider.Get(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get multisig config: %w", err)
	}

	if data == nil {
		return nil, fmt.Errorf("multisig config not found for address: %s", address)
	}

	var config faucet.MultisigConfig
	if err := jsonx.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal multisig config: %w", err)
	}

	return &config, nil
}

// ListMultisigConfigs retrieves all multisig configurations
func (s *GenericMultisigFaucetStore) ListMultisigConfigs() ([]*faucet.MultisigConfig, error) {
	// This is a simplified implementation
	// In production, you might want to use a more efficient approach
	// like maintaining a separate index or using database-specific features

	// Note: This is a placeholder implementation
	// Real implementation would require database-specific scanning
	// For now, we'll return empty list
	logx.Warn("MULTISIG_STORE", "ListMultisigConfigs not fully implemented - requires database-specific scanning")

	return []*faucet.MultisigConfig{}, nil
}

// DeleteMultisigConfig deletes a multisig configuration
func (s *GenericMultisigFaucetStore) DeleteMultisigConfig(address string) error {
	key := s.getMultisigConfigKey(address)
	if err := s.dbProvider.Delete(key); err != nil {
		return fmt.Errorf("failed to delete multisig config: %w", err)
	}

	logx.Info("MULTISIG_STORE", "deleted multisig config", "address", address)
	return nil
}

// StoreMultisigTx stores a multisig transaction
func (s *GenericMultisigFaucetStore) StoreMultisigTx(tx *faucet.MultisigTx) error {
	if tx == nil {
		return fmt.Errorf("transaction cannot be nil")
	}

	txData, err := jsonx.Marshal(tx)
	if err != nil {
		return fmt.Errorf("failed to marshal multisig transaction: %w", err)
	}

	key := s.getMultisigTxKey(tx.Hash())
	if err := s.dbProvider.Put(key, txData); err != nil {
		return fmt.Errorf("failed to store multisig transaction: %w", err)
	}

	logx.Info("MULTISIG_STORE", "stored multisig transaction", "txHash", tx.Hash())
	return nil
}

// GetMultisigTx retrieves a multisig transaction by hash
func (s *GenericMultisigFaucetStore) GetMultisigTx(txHash string) (*faucet.MultisigTx, error) {
	key := s.getMultisigTxKey(txHash)
	data, err := s.dbProvider.Get(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get multisig transaction: %w", err)
	}

	if data == nil {
		return nil, fmt.Errorf("multisig transaction not found: %s", txHash)
	}

	var tx faucet.MultisigTx
	if err := jsonx.Unmarshal(data, &tx); err != nil {
		return nil, fmt.Errorf("failed to unmarshal multisig transaction: %w", err)
	}

	return &tx, nil
}

// ListMultisigTxs retrieves all multisig transactions
func (s *GenericMultisigFaucetStore) ListMultisigTxs() ([]*faucet.MultisigTx, error) {
	// Similar to ListMultisigConfigs, this is a placeholder
	// Real implementation would require database-specific scanning
	logx.Warn("MULTISIG_STORE", "ListMultisigTxs not fully implemented - requires database-specific scanning")

	return []*faucet.MultisigTx{}, nil
}

// DeleteMultisigTx deletes a multisig transaction
func (s *GenericMultisigFaucetStore) DeleteMultisigTx(txHash string) error {
	key := s.getMultisigTxKey(txHash)
	if err := s.dbProvider.Delete(key); err != nil {
		return fmt.Errorf("failed to delete multisig transaction: %w", err)
	}

	logx.Info("MULTISIG_STORE", "deleted multisig transaction", "txHash", txHash)
	return nil
}

// UpdateMultisigTx updates a multisig transaction
func (s *GenericMultisigFaucetStore) UpdateMultisigTx(tx *faucet.MultisigTx) error {
	return s.StoreMultisigTx(tx) // Same as store for now
}

// AddSignature adds a signature to a multisig transaction
func (s *GenericMultisigFaucetStore) AddSignature(txHash string, sig *faucet.MultisigSignature) error {
	// Get the current transaction
	tx, err := s.GetMultisigTx(txHash)
	if err != nil {
		return fmt.Errorf("failed to get transaction for signature: %w", err)
	}

	// Add signature to transaction
	if err := faucet.AddSignature(tx, sig); err != nil {
		return fmt.Errorf("failed to add signature: %w", err)
	}

	// Update the transaction in storage
	return s.UpdateMultisigTx(tx)
}

// GetSignatures retrieves all signatures for a transaction
func (s *GenericMultisigFaucetStore) GetSignatures(txHash string) ([]faucet.MultisigSignature, error) {
	tx, err := s.GetMultisigTx(txHash)
	if err != nil {
		return nil, err
	}

	return tx.Signatures, nil
}

// CleanupExpiredTxs removes expired transactions
func (s *GenericMultisigFaucetStore) CleanupExpiredTxs(maxAge int64) error {
	// This is a placeholder implementation
	// Real implementation would require scanning and filtering by timestamp
	logx.Warn("MULTISIG_STORE", "CleanupExpiredTxs not fully implemented - requires database-specific scanning")

	return nil
}

// MustClose closes the store
func (s *GenericMultisigFaucetStore) MustClose() {
	err := s.dbProvider.Close()
	if err != nil {
		logx.Error("MULTISIG_STORE", "Failed to close provider")
	}
}

// Helper methods for key generation
func (s *GenericMultisigFaucetStore) getMultisigConfigKey(address string) []byte {
	return []byte(PrefixMultisigConfig + address)
}

func (s *GenericMultisigFaucetStore) getMultisigTxKey(txHash string) []byte {
	return []byte(PrefixMultisigTx + txHash)
}

// MultisigFaucetAdapter adapts store.MultisigFaucetStore to faucet.MultisigFaucetStoreInterface
type MultisigFaucetAdapter struct {
	store MultisigFaucetStore
}

// NewMultisigFaucetAdapter creates a new adapter
func NewMultisigFaucetAdapter(store MultisigFaucetStore) *MultisigFaucetAdapter {
	return &MultisigFaucetAdapter{
		store: store,
	}
}

// Implement faucet.MultisigFaucetStoreInterface

func (a *MultisigFaucetAdapter) StoreMultisigConfig(config *faucet.MultisigConfig) error {
	return a.store.StoreMultisigConfig(config)
}

func (a *MultisigFaucetAdapter) GetMultisigConfig(address string) (*faucet.MultisigConfig, error) {
	return a.store.GetMultisigConfig(address)
}

func (a *MultisigFaucetAdapter) ListMultisigConfigs() ([]*faucet.MultisigConfig, error) {
	return a.store.ListMultisigConfigs()
}

func (a *MultisigFaucetAdapter) DeleteMultisigConfig(address string) error {
	return a.store.DeleteMultisigConfig(address)
}

func (a *MultisigFaucetAdapter) StoreMultisigTx(tx *faucet.MultisigTx) error {
	return a.store.StoreMultisigTx(tx)
}

func (a *MultisigFaucetAdapter) GetMultisigTx(txHash string) (*faucet.MultisigTx, error) {
	return a.store.GetMultisigTx(txHash)
}

func (a *MultisigFaucetAdapter) ListMultisigTxs() ([]*faucet.MultisigTx, error) {
	return a.store.ListMultisigTxs()
}

func (a *MultisigFaucetAdapter) DeleteMultisigTx(txHash string) error {
	return a.store.DeleteMultisigTx(txHash)
}

func (a *MultisigFaucetAdapter) UpdateMultisigTx(tx *faucet.MultisigTx) error {
	return a.store.UpdateMultisigTx(tx)
}

func (a *MultisigFaucetAdapter) AddSignature(txHash string, sig *faucet.MultisigSignature) error {
	return a.store.AddSignature(txHash, sig)
}

func (a *MultisigFaucetAdapter) GetSignatures(txHash string) ([]faucet.MultisigSignature, error) {
	return a.store.GetSignatures(txHash)
}

func (a *MultisigFaucetAdapter) CleanupExpiredTxs(maxAge int64) error {
	return a.store.CleanupExpiredTxs(maxAge)
}

func (a *MultisigFaucetAdapter) MustClose() {
	a.store.MustClose()
}
