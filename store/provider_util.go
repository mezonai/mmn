package store

import "github.com/mezonai/mmn/db"

// GetProviderFromAccountStore returns the underlying DatabaseProvider when using GenericAccountStore.
// Returns nil if not available.
func GetProviderFromAccountStore(as AccountStore) db.DatabaseProvider {
	if g, ok := as.(*GenericAccountStore); ok {
		return g.dbProvider
	}
	return nil
}
