package store

import (
	"fmt"
	"sync"

	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/types"
)

type AccountStore interface {
	Store(account *types.Account) error
	StoreBatch(accounts []*types.Account) error
	GetByAddr(addr string) (*types.Account, error)
	ExistsByAddr(addr string) (bool, error)
	MustClose()
}

type GenericAccountStore struct {
	mu         sync.RWMutex
	dbProvider db.DatabaseProvider
}

func NewGenericAccountStore(dbProvider db.DatabaseProvider) (*GenericAccountStore, error) {
	if dbProvider == nil {
		return nil, fmt.Errorf("provider cannot be nil")
	}

	return &GenericAccountStore{
		dbProvider: dbProvider,
	}, nil
}

func (as *GenericAccountStore) Store(account *types.Account) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	accountData, err := jsonx.Marshal(account)
	if err != nil {
		return fmt.Errorf("%w failed to marshal account: %w", ErrFailedMarshalAccount, err)
	}

	err = as.dbProvider.Put(as.getDbKey(account.Address), accountData)
	if err != nil {
		return fmt.Errorf("%w failed to write account to db: %w", ErrFaliedWriteAccount, err)
	}

	return nil
}

func (as *GenericAccountStore) StoreBatch(accounts []*types.Account) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	batch := as.dbProvider.Batch()
	defer batch.Close()

	for _, account := range accounts {
		accountData, err := jsonx.Marshal(account)
		if err != nil {
			return fmt.Errorf("failed to marshal account: %w", err)
		}

		batch.Put(as.getDbKey(account.Address), accountData)
	}

	err := batch.Write()
	if err != nil {
		return fmt.Errorf("failed to write batch of accounts to database: %w", err)
	}

	return nil
}

// GetByAddr returns account instance from db, return both nil if not exist
func (as *GenericAccountStore) GetByAddr(addr string) (*types.Account, error) {
	as.mu.RLock()
	defer as.mu.RUnlock()

	data, err := as.dbProvider.Get(as.getDbKey(addr))
	if err != nil {
		return nil, fmt.Errorf("could not get account %s from db: %w", addr, err)
	}

	// Account doesn't exist
	if data == nil {
		return nil, nil
	}

	// Deserialize account
	var acc types.Account
	err = jsonx.Unmarshal(data, &acc)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal account %s: %w", addr, err)
	}

	return &acc, nil
}

func (as *GenericAccountStore) ExistsByAddr(addr string) (bool, error) {
	as.mu.RLock()
	defer as.mu.RUnlock()

	return as.dbProvider.Has(as.getDbKey(addr))
}

func (as *GenericAccountStore) MustClose() {
	err := as.dbProvider.Close()
	if err != nil {
		logx.Error("ACCOUNT_STORE", "Failed to close db provider:", err.Error())
	}
}

func (as *GenericAccountStore) getDbKey(addr string) []byte {
	return []byte(PrefixAccount + addr)
}

var ErrFailedMarshalAccount = fmt.Errorf("failed marshal account")
var ErrFaliedWriteAccount = fmt.Errorf("failed write account")
