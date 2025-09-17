package store

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/logx"
	pb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/types"
	"github.com/mezonai/mmn/utils"
	"google.golang.org/protobuf/proto"
)

type AccountStore interface {
	Store(account *types.Account) error
	StoreBatch(accounts []*types.Account) error
	GetByAddr(addr string) (*types.Account, error)
	GetBatch(addrs []string) (map[string]*types.Account, error)
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

	// Marshal as protobuf (compact); history is not persisted in this format
	pbAcc := &pb.AccountData{
		Address: account.Address,
		Balance: utils.Uint256ToString(account.Balance),
		Nonce:   account.Nonce,
	}
	accountData, err := proto.Marshal(pbAcc)
	if err != nil {
		return fmt.Errorf("failed to marshal account (proto): %w", err)
	}

	err = as.dbProvider.Put(as.getDbKey(account.Address), accountData)
	if err != nil {
		return fmt.Errorf("failed to write account to db: %w", err)
	}

	return nil
}

func (as *GenericAccountStore) StoreBatch(accounts []*types.Account) error {
	as.mu.Lock()
	defer as.mu.Unlock()

	batch := as.dbProvider.Batch()
	for _, account := range accounts {
		pbAcc := &pb.AccountData{
			Address: account.Address,
			Balance: utils.Uint256ToString(account.Balance),
			Nonce:   account.Nonce,
		}
		accountData, err := proto.Marshal(pbAcc)
		if err != nil {
			return fmt.Errorf("failed to marshal account (proto): %w", err)
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

	// Try protobuf first
	var pbAcc pb.AccountData
	if err := proto.Unmarshal(data, &pbAcc); err == nil && pbAcc.Address != "" {
		return &types.Account{
			Address: pbAcc.Address,
			Balance: utils.Uint256FromString(pbAcc.Balance),
			Nonce:   pbAcc.Nonce,
		}, nil
	}

	// Fallback to JSON for backward compatibility
	var acc types.Account
	if err := json.Unmarshal(data, &acc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account %s: %w", addr, err)
	}
	return &acc, nil
}

// GetBatch retrieves multiple accounts by addresses. Missing accounts return as nil entries.
func (as *GenericAccountStore) GetBatch(addrs []string) (map[string]*types.Account, error) {
	as.mu.RLock()
	defer as.mu.RUnlock()

	result := make(map[string]*types.Account, len(addrs))
	for _, addr := range addrs {
		if addr == "" {
			continue
		}
		data, err := as.dbProvider.Get(as.getDbKey(addr))
		if err != nil {
			return nil, fmt.Errorf("could not get account %s from db: %w", addr, err)
		}
		if data == nil {
			result[addr] = nil
			continue
		}
		// Try protobuf first
		var pbAcc pb.AccountData
		if err := proto.Unmarshal(data, &pbAcc); err == nil && pbAcc.Address != "" {
			result[addr] = &types.Account{
				Address: pbAcc.Address,
				Balance: utils.Uint256FromString(pbAcc.Balance),
				Nonce:   pbAcc.Nonce,
			}
			continue
		}
		// Fallback to JSON for backward compatibility
		var acc types.Account
		if err := json.Unmarshal(data, &acc); err != nil {
			return nil, fmt.Errorf("failed to unmarshal account %s: %w", addr, err)
		}
		result[addr] = &acc
	}
	return result, nil
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
