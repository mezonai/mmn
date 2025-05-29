// File: pkg/contract/dynamic.go
package contract

import (
	"errors"
	"fmt"
	"sync"
)

// ContractDefinition holds the code and metadata for a deployed contract.
type ContractDefinition struct {
	Name string
	Code []byte // For example, WASM bytecode.
	// Additional metadata such as initial state can be added here.
}

// DynamicRegistry is a thread-safe registry for deployed contracts.
type DynamicRegistry struct {
	contracts map[string]ContractDefinition
	mu        sync.RWMutex
}

// NewDynamicRegistry creates and returns a new dynamic contract registry.
func NewDynamicRegistry() *DynamicRegistry {
	return &DynamicRegistry{
		contracts: make(map[string]ContractDefinition),
	}
}

// RegisterContract deploys a new contract by adding it to the registry.
func (dr *DynamicRegistry) RegisterContract(def ContractDefinition) error {
	dr.mu.Lock()
	defer dr.mu.Unlock()
	if _, exists := dr.contracts[def.Name]; exists {
		return errors.New("contract already exists")
	}
	dr.contracts[def.Name] = def
	fmt.Printf("Dynamic contract '%s' registered successfully.\n", def.Name)
	return nil
}

// GetContract retrieves a contract definition by name.
func (dr *DynamicRegistry) GetContract(name string) (ContractDefinition, error) {
	dr.mu.RLock()
	defer dr.mu.RUnlock()
	def, exists := dr.contracts[name]
	if !exists {
		return ContractDefinition{}, errors.New("contract not found")
	}
	return def, nil
}
