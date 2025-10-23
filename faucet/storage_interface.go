package faucet

import "github.com/mezonai/mmn/types"

// MultisigFaucetStoreInterface defines the interface for multisig faucet storage
// This interface is defined in the faucet package to avoid import cycles
type MultisigFaucetStoreInterface interface {
	// Multisig Config operations
	StoreMultisigConfig(config *types.MultisigConfig) error
	GetMultisigConfig(address string) (*types.MultisigConfig, error)
	ListMultisigConfigs() ([]*types.MultisigConfig, error)
	DeleteMultisigConfig(address string) error

	// Multisig Transaction operations
	StoreMultisigTx(tx *types.MultisigTx) error
	GetMultisigTx(txHash string) (*types.MultisigTx, error)
	ListMultisigTxs() ([]*types.MultisigTx, error)
	DeleteMultisigTx(txHash string) error
	UpdateMultisigTx(tx *types.MultisigTx) error

	// Signature operations
	AddSignature(txHash string, sig *types.MultisigSignature) error
	GetSignatures(txHash string) ([]types.MultisigSignature, error)

	// Cleanup operations
	CleanupExpiredTxs(maxAge int64) error

	// Close operations
	MustClose()
}
