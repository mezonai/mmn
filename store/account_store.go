package store

import (
	"fmt"
	"sync"

	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/errors"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/types"
)

type AccountStore interface {
	Store(account *types.Account) error
	StoreBatch(accounts []*types.Account) error
	GetByAddr(addr string) (*types.Account, error)
	GetBatch(addrs []string) (map[string]*types.Account, error)
	ExistsByAddr(addr string) (bool, error)
	GetDBKey(addr string) []byte
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

	err = as.dbProvider.Put(as.GetDBKey(account.Address), accountData)
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

		batch.Put(as.GetDBKey(account.Address), accountData)
	}

	err := batch.Write()
	if err != nil {
		return fmt.Errorf("failed to write batch of accounts to database: %w", err)
	}

	return nil
}

// GetByAddr returns account instance from db, return both nil if not exist
func (as *GenericAccountStore) GetByAddr(addr string) (*types.Account, error) {
	data, err := as.dbProvider.Get(as.GetDBKey(addr))
	if err != nil {
		logx.Error("ACCOUNT_STORE", fmt.Sprintf("could not get account %s from db: %v", addr, err))
		return nil, errors.NewError(errors.ErrCodeInternal, errors.ErrMsgInternal)
	}

	// Account doesn't exist
	if data == nil {
		return nil, nil
	}

	// Deserialize account
	var acc types.Account
	err = jsonx.Unmarshal(data, &acc)
	if err != nil {
		logx.Error("ACCOUNT_STORE", fmt.Sprintf("failed to unmarshal account %s: %v", addr, err))
		return nil, errors.NewError(errors.ErrCodeInternal, errors.ErrMsgInternal)
	}

	return &acc, nil
}

// GetBatch retrieves multiple accounts by their addresses using batch operation
func (as *GenericAccountStore) GetBatch(addrs []string) (map[string]*types.Account, error) {
	if len(addrs) == 0 {
		return make(map[string]*types.Account), nil
	}

	// Prepare keys for batch operation
	keys := make([][]byte, len(addrs))
	keyToAddr := make(map[string]string, len(addrs))
	for i, addr := range addrs {
		key := as.GetDBKey(addr)
		keys[i] = key
		keyToAddr[string(key)] = addr
	}

	// Use batch read - single CGO call!
	dataMap, err := as.dbProvider.GetBatch(keys)
	if err != nil {
		return nil, fmt.Errorf("failed to batch get accounts: %w", err)
	}

	accounts := make(map[string]*types.Account, len(addrs))
	for keyStr, data := range dataMap {
		addr := keyToAddr[keyStr]

		// Deserialize account
		var acc types.Account
		err = jsonx.Unmarshal(data, &acc)
		if err != nil {
			logx.Warn("ACCOUNT_STORE", fmt.Sprintf("Failed to unmarshal account %s: %s", addr, err.Error()))
			continue
		}

		accounts[addr] = &acc
	}

	return accounts, nil
}

func (as *GenericAccountStore) ExistsByAddr(addr string) (bool, error) {
	return as.dbProvider.Has(as.GetDBKey(addr))
}

func (as *GenericAccountStore) MustClose() {
	err := as.dbProvider.Close()
	if err != nil {
		logx.Error("ACCOUNT_STORE", "Failed to close db provider:", err.Error())
	}
}

func (as *GenericAccountStore) GetDBKey(addr string) []byte {
	return []byte(PrefixAccount + addr)
}

var ErrFailedMarshalAccount = fmt.Errorf("failed marshal account")
var ErrFaliedWriteAccount = fmt.Errorf("failed write account")
