package blockstore

import (
	"encoding/json"
	"fmt"
	"github.com/mezonai/mmn/types"
	"sync"
)

type AccountStore interface {
	Store(account *types.Account) error
	GetByAddr(addr string) (*types.Account, error)
	ExistsByAddr(addr string) (bool, error)
	Close() error
}

type GenericAccountStore struct {
	mu         sync.RWMutex
	dbProvider DatabaseProvider
}

func NewGenericAccountStore(dbProvider DatabaseProvider) (*GenericAccountStore, error) {
	if dbProvider == nil {
		return nil, fmt.Errorf("provider cannot be nil")
	}

	return &GenericAccountStore{
		dbProvider: dbProvider,
	}, nil
}

func (as *GenericAccountStore) Store(account *types.Account) error {
	as.mu.RLock()
	defer as.mu.RUnlock()

	accountData, err := json.Marshal(account)
	if err != nil {
		return fmt.Errorf("failed to marshal account: %w", err)
	}

	err = as.dbProvider.Put([]byte(account.Address), accountData)
	if err != nil {
		return fmt.Errorf("failed to write account to db: %w", err)
	}

	return nil
}

// GetByAddr returns account instance from db, return both nil if not exist
func (as *GenericAccountStore) GetByAddr(addr string) (*types.Account, error) {
	as.mu.RLock()
	defer as.mu.RUnlock()

	data, err := as.dbProvider.Get([]byte(addr))
	if err != nil {
		return nil, fmt.Errorf("could not get account %s from db: %w", addr, err)
	}

	// Account doesn't exist
	if data == nil {
		return nil, nil
	}

	// Deserialize account
	var acc types.Account
	err = json.Unmarshal(data, &acc)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal account %s: %w", addr, err)
	}

	return &acc, nil
}

func (as *GenericAccountStore) ExistsByAddr(addr string) (bool, error) {
	as.mu.RLock()
	defer as.mu.RUnlock()

	return as.dbProvider.Has([]byte(addr))
}

func (as *GenericAccountStore) Close() error {
	return as.dbProvider.Close()
}
