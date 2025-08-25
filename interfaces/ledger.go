package interfaces

import (
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/types"
)

// Ledger interface defines the methods required for ledger operations
type Ledger interface {
	// AccountExists checks if an account exists
	AccountExists(addr string) (bool, error)
	// GetAccount returns the account for the given address
	GetAccount(addr string) (*types.Account, error)
	// Balance returns the balance for the given address
	Balance(addr string) (uint64, error)
	// CreateAccountsFromGenesis creates an account from genesis block
	CreateAccountsFromGenesis(addrs []config.Address) error
}
