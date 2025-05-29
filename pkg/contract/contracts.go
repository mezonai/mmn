// File: pkg/contract/contracts.go
package contract

import (
	"errors"
	"fmt"
)

// Contract is an interface that all smart contracts must implement.
// It now supports multiple methods via an additional "method" parameter.
type Contract interface {
	// Execute runs the contract logic based on a method name and provided input parameters.
	// It returns a result and/or an error.
	Execute(method string, params map[string]interface{}) (interface{}, error)
	// Name returns the unique name of the contract.
	Name() string
}

// ContractRegistry holds all deployed contracts.
var ContractRegistry = make(map[string]Contract)

// RegisterContract adds a new contract to the registry.
func RegisterContract(c Contract) error {
	name := c.Name()
	if _, exists := ContractRegistry[name]; exists {
		return errors.New("contract already exists")
	}
	ContractRegistry[name] = c
	fmt.Printf("Contract '%s' registered successfully.\n", name)
	return nil
}

// ExecuteContract looks up a contract by name and executes it using the given method and parameters.
func ExecuteContract(name string, method string, params map[string]interface{}) (interface{}, error) {
	contract, exists := ContractRegistry[name]
	if !exists {
		return nil, errors.New("contract not found")
	}
	return contract.Execute(method, params)
}

// --- Example Contract Implementation ---

// AdditionContract is a sample contract that adds two numbers.
type AdditionContract struct{}

// Execute processes the "add" method by adding two parameters "a" and "b".
// If a method other than "add" is passed, it returns an error.
func (ac AdditionContract) Execute(method string, params map[string]interface{}) (interface{}, error) {
	if method != "add" {
		return nil, errors.New("unsupported method")
	}
	aVal, ok := params["a"].(float64)
	if !ok {
		return nil, errors.New("invalid or missing parameter: a")
	}
	bVal, ok := params["b"].(float64)
	if !ok {
		return nil, errors.New("invalid or missing parameter: b")
	}
	result := aVal + bVal
	return result, nil
}

// Name returns the unique name of the contract.
func (ac AdditionContract) Name() string {
	return "AdditionContract"
}
