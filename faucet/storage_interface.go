package faucet

// MultisigFaucetStoreInterface defines the interface for multisig faucet storage
// This interface is defined in the faucet package to avoid import cycles
type MultisigFaucetStoreInterface interface {
	// Multisig Config operations
	StoreMultisigConfig(config *MultisigConfig) error
	GetMultisigConfig(address string) (*MultisigConfig, error)
	ListMultisigConfigs() ([]*MultisigConfig, error)
	DeleteMultisigConfig(address string) error

	// Multisig Transaction operations
	StoreMultisigTx(tx *MultisigTx) error
	GetMultisigTx(txHash string) (*MultisigTx, error)
	ListMultisigTxs() ([]*MultisigTx, error)
	DeleteMultisigTx(txHash string) error
	UpdateMultisigTx(tx *MultisigTx) error

	// Signature operations
	AddSignature(txHash string, sig *MultisigSignature) error
	GetSignatures(txHash string) ([]MultisigSignature, error)

	// Cleanup operations
	CleanupExpiredTxs(maxAge int64) error

	// Close operations
	MustClose()
}
