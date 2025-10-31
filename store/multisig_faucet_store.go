package store

import (
	"fmt"

	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/types"
)

type MultisigFaucetStore interface {
	StoreMultisigConfig(config *types.MultisigConfig) error
	GetMultisigConfig(address string) (*types.MultisigConfig, error)

	StoreMultisigTx(tx *types.MultisigTx) error
	GetMultisigTx(txHash string) (*types.MultisigTx, error)
	ListMultisigTxs() ([]*types.MultisigTx, error)
	UpdateMultisigTx(tx *types.MultisigTx) error
	StoreMultisigTxs(txs []*types.MultisigTx) error

	AddSignature(txHash string, sig *types.MultisigSignature) error

	// Whitelist management
	StoreApproverWhitelist(addresses []string) error
	GetApproverWhitelist() ([]string, error)
	StoreProposerWhitelist(addresses []string) error
	GetProposerWhitelist() ([]string, error)

	MustClose()
}

type GenericMultisigFaucetStore struct {
	dbProvider db.DatabaseProvider
}

func NewGenericMultisigFaucetStore(dbProvider db.DatabaseProvider) (*GenericMultisigFaucetStore, error) {
	if dbProvider == nil {
		return nil, fmt.Errorf("provider cannot be nil")
	}

	return &GenericMultisigFaucetStore{
		dbProvider: dbProvider,
	}, nil
}

func (s *GenericMultisigFaucetStore) StoreMultisigConfig(config *types.MultisigConfig) error {
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

	return nil
}

func (s *GenericMultisigFaucetStore) GetMultisigConfig(address string) (*types.MultisigConfig, error) {
	key := s.getMultisigConfigKey(address)
	data, err := s.dbProvider.Get(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get multisig config: %w", err)
	}

	if data == nil {
		return nil, fmt.Errorf("multisig config not found for address: %s", address)
	}

	var config types.MultisigConfig
	if err := jsonx.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal multisig config: %w", err)
	}

	return &config, nil
}

func (s *GenericMultisigFaucetStore) StoreMultisigTx(tx *types.MultisigTx) error {
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

func (s *GenericMultisigFaucetStore) GetMultisigTx(txHash string) (*types.MultisigTx, error) {
	key := s.getMultisigTxKey(txHash)
	data, err := s.dbProvider.Get(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get multisig transaction: %w", err)
	}

	if data == nil {
		return nil, fmt.Errorf("multisig transaction not found: %s", txHash)
	}

	var tx types.MultisigTx
	if err := jsonx.Unmarshal(data, &tx); err != nil {
		return nil, fmt.Errorf("failed to unmarshal multisig transaction: %w", err)
	}

	return &tx, nil
}

func (s *GenericMultisigFaucetStore) ListMultisigTxs() ([]*types.MultisigTx, error) {
	iterableProvider, ok := s.dbProvider.(db.IterableProvider)
	if !ok {
		return nil, fmt.Errorf("database provider does not support iteration")
	}

	var txs []*types.MultisigTx
	prefix := []byte(PrefixMultisigTx)

	err := iterableProvider.IteratePrefix(prefix, func(key, value []byte) bool {
		var tx types.MultisigTx
		if err := jsonx.Unmarshal(value, &tx); err != nil {
			logx.Error("MULTISIG_STORE", "failed to unmarshal multisig transaction", "error", err)
			return true
		}
		txs = append(txs, &tx)
		return true
	})

	if err != nil {
		return nil, fmt.Errorf("failed to iterate multisig transactions: %w", err)
	}

	return txs, nil
}

func (s *GenericMultisigFaucetStore) UpdateMultisigTx(tx *types.MultisigTx) error {
	return s.StoreMultisigTx(tx)
}

func (s *GenericMultisigFaucetStore) AddSignature(txHash string, sig *types.MultisigSignature) error {
	if sig == nil {
		return fmt.Errorf("signature cannot be nil")
	}

	if sig.Signer == "" {
		return fmt.Errorf("signer cannot be empty")
	}

	if sig.Signature == "" {
		return fmt.Errorf("signature data cannot be empty")
	}

	tx, err := s.GetMultisigTx(txHash)
	if err != nil {
		return fmt.Errorf("failed to get transaction for signature: %w", err)
	}

	authorized := false
	for _, authorizedSigner := range tx.Config.Signers {
		if authorizedSigner == sig.Signer {
			authorized = true
			break
		}
	}

	if !authorized {
		return fmt.Errorf("signer %s is not authorized for this multisig transaction", sig.Signer)
	}

	for _, existingSig := range tx.Signatures {
		if existingSig.Signer == sig.Signer {
			return fmt.Errorf("signature from signer %s already exists", sig.Signer)
		}
	}

	tx.Signatures = append(tx.Signatures, *sig)

	if err := s.UpdateMultisigTx(tx); err != nil {
		return fmt.Errorf("failed to update transaction with new signature: %w", err)
	}

	return nil
}

func (s *GenericMultisigFaucetStore) MustClose() {
	err := s.dbProvider.Close()
	if err != nil {
		logx.Error("MULTISIG_STORE", "Failed to close provider")
	}
}

func (s *GenericMultisigFaucetStore) getMultisigConfigKey(address string) []byte {
	return []byte(PrefixMultisigConfig + address)
}

func (s *GenericMultisigFaucetStore) getMultisigTxKey(txHash string) []byte {
	return []byte(PrefixMultisigTx + txHash)
}

func (s *GenericMultisigFaucetStore) StoreApproverWhitelist(addresses []string) error {
	whitelistData, err := jsonx.Marshal(addresses)
	if err != nil {
		return fmt.Errorf("failed to marshal approver whitelist: %w", err)
	}

	key := []byte(PrefixApprover)
	if err := s.dbProvider.Put(key, whitelistData); err != nil {
		return fmt.Errorf("failed to store approver whitelist: %w", err)
	}

	logx.Info("MULTISIG_STORE", "stored approver whitelist", "count", len(addresses))
	return nil
}

func (s *GenericMultisigFaucetStore) GetApproverWhitelist() ([]string, error) {
	key := []byte(PrefixApprover)
	data, err := s.dbProvider.Get(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get approver whitelist: %w", err)
	}

	if data == nil {
		return []string{}, nil
	}

	var addresses []string
	if err := jsonx.Unmarshal(data, &addresses); err != nil {
		return nil, fmt.Errorf("failed to unmarshal approver whitelist: %w", err)
	}

	return addresses, nil
}

func (s *GenericMultisigFaucetStore) StoreProposerWhitelist(addresses []string) error {
	whitelistData, err := jsonx.Marshal(addresses)
	if err != nil {
		return fmt.Errorf("failed to marshal proposer whitelist: %w", err)
	}

	key := []byte(PrefixProposer)
	if err := s.dbProvider.Put(key, whitelistData); err != nil {
		return fmt.Errorf("failed to store proposer whitelist: %w", err)
	}

	logx.Info("MULTISIG_STORE", "stored proposer whitelist", "count", len(addresses))
	return nil
}

func (s *GenericMultisigFaucetStore) GetProposerWhitelist() ([]string, error) {
	key := []byte(PrefixProposer)
	data, err := s.dbProvider.Get(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get proposer whitelist: %w", err)
	}

	if data == nil {
		return []string{}, nil
	}

	var addresses []string
	if err := jsonx.Unmarshal(data, &addresses); err != nil {
		return nil, fmt.Errorf("failed to unmarshal proposer whitelist: %w", err)
	}

	return addresses, nil
}

func (s *GenericMultisigFaucetStore) StoreMultisigTxs(txs []*types.MultisigTx) error {
	batch := s.dbProvider.Batch()
	defer batch.Close()

	for _, tx := range txs {
		txData, err := jsonx.Marshal(tx)
		if err != nil {
			return fmt.Errorf("failed to marshal multisig transaction: %w", err)
		}
		batch.Put(s.getMultisigTxKey(tx.Hash()), txData)
	}

	err := batch.Write()
	if err != nil {
		return fmt.Errorf("failed to write multisig transactions to database: %w", err)
	}

	return nil
}
