package faucet

import (
	"github.com/mezonai/mmn/types"
)


type MultisigFaucetStoreInterface interface {
	StoreMultisigConfig(config *types.MultisigConfig) error
	GetMultisigConfig(address string) (*types.MultisigConfig, error)
	ListMultisigConfigs() ([]*types.MultisigConfig, error)
	DeleteMultisigConfig(address string) error

	StoreMultisigTx(tx *types.MultisigTx) error
	GetMultisigTx(txHash string) (*types.MultisigTx, error)
	ListMultisigTxs() ([]*types.MultisigTx, error)
	DeleteMultisigTx(txHash string) error
	UpdateMultisigTx(tx *types.MultisigTx) error

	AddSignature(txHash string, sig *types.MultisigSignature) error
	GetSignatures(txHash string) ([]types.MultisigSignature, error)

	CleanupExpiredTxs(maxAge int64) error

	// Query operations
	GetMultisigTxsByStatus(status string) ([]*types.MultisigTx, error)
	GetMultisigTxsBySigner(signer string) ([]*types.MultisigTx, error)
	IsTransactionExecutable(txHash string) (bool, error)

	MustClose()
}

var (
	CREATE_FAUCET                = "CREATE_FAUCET"
	FAUCET_ACTION                = "FAUCET_ACTION"
	ADD_SIGNATURE                = "ADD_SIGNATURE"
	ADD_APPROVER                 = "ADD_APPROVER"
	REMOVE_APPROVER              = "REMOVE_APPROVER"
	ADD_PROPOSER                 = "ADD_PROPOSER"
	REMOVE_PROPOSER              = "REMOVE_PROPOSER"
	EXECUTE_WHITELIST_MANAGEMENT = "EXECUTE_WHITELIST_MANAGEMENT"
	WHITELIST_MANAGEMENT_PREFIX  = "WHITELIST_MANAGEMENT:"
)

var (
	STATUS_EXECUTED = "EXECUTED"
	STATUS_PENDING  = "PENDING"
	STATUS_FAILED   = "FAILED"
)


// # Proposer management (cần multisig approval)
// ./mmn multisig add-proposer --address "ADDRESS" --private-key-file "key.txt"
// ./mmn multisig remove-proposer --address "ADDRESS" --private-key-file "key.txt"

// # Approver management (cần multisig approval)  
// ./mmn multisig add-approver --address "ADDRESS" --private-key-file "key.txt"
// ./mmn multisig remove-approver --address "ADDRESS" --private-key-file "key.txt"