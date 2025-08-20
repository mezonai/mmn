package interfaces

import (
	"github.com/mezonai/mmn/types"
)

// Ledger interface defines the methods required for ledger operations
type Ledger interface {
	// AccountExists checks if an account exists
	AccountExists(addr string) bool
	// GetAccount returns the account for the given address
	GetAccount(addr string) *types.Account
	// Balance returns the balance for the given address
	Balance(addr string) uint64
	// CreateAccountFromGenesis creates an account from genesis block
	CreateAccountFromGenesis(addr string, balance uint64) error
}