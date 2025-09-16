package ledger

import (
	"context"
	"fmt"
	"sync"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/types"
)

type TransactionDependency struct {
	ReadAccounts  []string
	WriteAccounts []string
	Tx            *transaction.Transaction
}

type ExecutionResult struct {
	Tx           *transaction.Transaction
	Success      bool
	Error        error
	UpdatedState map[string]*types.Account
	TxMeta       *types.TransactionMeta
}

type ParallelExecutor struct {
	workerCount int
	results     chan ExecutionResult
	errors      chan error
	mu          sync.RWMutex
}

func NewParallelExecutor() *ParallelExecutor {
	// Default fixed worker count for predictable performance
	defaultWorkers := 32
	if defaultWorkers < 1 {
		defaultWorkers = 1
	}

	return &ParallelExecutor{
		workerCount: defaultWorkers,
		results:     make(chan ExecutionResult, 1000),
		errors:      make(chan error, 1000),
	}
}

// SetWorkerCount allows tuning executor parallelism at runtime.
func (pe *ParallelExecutor) SetWorkerCount(n int) {
	if n <= 0 {
		return
	}
	if n > 64 {
		n = 64
	}
	pe.mu.Lock()
	pe.workerCount = n
	pe.mu.Unlock()
}

func (pe *ParallelExecutor) AnalyzeDependencies(txs []*transaction.Transaction) []TransactionDependency {
	dependencies := make([]TransactionDependency, len(txs))

	for i, tx := range txs {
		readAccounts := []string{tx.Sender, tx.Recipient}
		writeAccounts := []string{tx.Sender, tx.Recipient}

		dependencies[i] = TransactionDependency{
			ReadAccounts:  readAccounts,
			WriteAccounts: writeAccounts,
			Tx:            tx,
		}
	}

	return dependencies
}

func (pe *ParallelExecutor) BuildDependencyGraph(dependencies []TransactionDependency) map[int][]int {
	graph := make(map[int][]int)

	for i, dep1 := range dependencies {
		graph[i] = []int{}

		for j, dep2 := range dependencies {
			if i == j {
				continue
			}

			if pe.hasWriteWriteConflict(dep1, dep2) {
				graph[i] = append(graph[i], j)
			}
		}
	}

	return graph
}

func (pe *ParallelExecutor) hasWriteWriteConflict(dep1, dep2 TransactionDependency) bool {
	writeSet1 := make(map[string]bool)
	for _, acc := range dep1.WriteAccounts {
		writeSet1[acc] = true
	}

	for _, acc := range dep2.WriteAccounts {
		if writeSet1[acc] {
			return true
		}
	}

	return false
}

func (pe *ParallelExecutor) ExecuteParallel(
	ctx context.Context,
	dependencies []TransactionDependency,
	accountStore store.AccountStore,
	eventRouter *events.EventRouter,
	slot uint64,
	blockHash string,
) ([]ExecutionResult, error) {

	graph := pe.BuildDependencyGraph(dependencies)

	executionGroups := pe.groupByDependencyLevel(graph, len(dependencies))

	var allResults []ExecutionResult
	var mu sync.Mutex

	// Execute each group in parallel
	for _, group := range executionGroups {
		groupResults := pe.executeGroup(ctx, group, dependencies, accountStore, eventRouter, slot, blockHash)

		mu.Lock()
		allResults = append(allResults, groupResults...)
		mu.Unlock()
	}

	return allResults, nil
}

func (pe *ParallelExecutor) groupByDependencyLevel(graph map[int][]int, totalTxs int) [][]int {
	groups := [][]int{}
	processed := make(map[int]bool)

	for len(processed) < totalTxs {
		var currentGroup []int

		for i := 0; i < totalTxs; i++ {
			if processed[i] {
				continue
			}

			// Check if all dependencies are processed
			canExecute := true
			for _, dep := range graph[i] {
				if !processed[dep] {
					canExecute = false
					break
				}
			}

			if canExecute {
				currentGroup = append(currentGroup, i)
			}
		}

		if len(currentGroup) == 0 {
			logx.Warn("PARALLEL_EXECUTOR", "Circular dependency detected, executing remaining transactions")
			for i := 0; i < totalTxs; i++ {
				if !processed[i] {
					currentGroup = append(currentGroup, i)
				}
			}
			// If still no transactions, break to avoid infinite loop
			if len(currentGroup) == 0 {
				break
			}
		}

		groups = append(groups, currentGroup)
		for _, txIdx := range currentGroup {
			processed[txIdx] = true
		}
	}

	return groups
}

func (pe *ParallelExecutor) executeGroup(
	ctx context.Context,
	group []int,
	dependencies []TransactionDependency,
	accountStore store.AccountStore,
	eventRouter *events.EventRouter,
	slot uint64,
	blockHash string,
) []ExecutionResult {

	var wg sync.WaitGroup
	results := make([]ExecutionResult, len(group))

	// Create worker pool
	semaphore := make(chan struct{}, pe.workerCount)

	for i, txIdx := range group {
		wg.Add(1)
		go func(index, txIndex int) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-semaphore }()

			// Execute transaction
			result := pe.executeTransaction(
				dependencies[txIndex],
				accountStore,
				eventRouter,
				slot,
				blockHash,
			)

			results[index] = result
		}(i, txIdx)
	}

	wg.Wait()
	return results
}

func (pe *ParallelExecutor) executeTransaction(
	dep TransactionDependency,
	accountStore store.AccountStore,
	eventRouter *events.EventRouter,
	slot uint64,
	blockHash string,
) ExecutionResult {

	tx := dep.Tx
	state := make(map[string]*types.Account)

	// Load account states
	for _, addr := range dep.ReadAccounts {
		acc, err := accountStore.GetByAddr(addr)
		if err != nil {
			return ExecutionResult{
				Tx:      tx,
				Success: false,
				Error:   fmt.Errorf("failed to load account %s: %w", addr, err),
			}
		}
		if acc == nil {
			// Create account if doesn't exist
			acc = &types.Account{
				Address: addr,
				Balance: uint256.NewInt(0),
				Nonce:   0,
			}
		}
		state[addr] = acc
	}

	// Apply transaction
	if err := applyTx(state, tx); err != nil {
		if eventRouter != nil {
			event := events.NewTransactionFailed(tx, fmt.Sprintf("transaction application failed: %v", err))
			eventRouter.PublishTransactionEvent(event)
		}

		return ExecutionResult{
			Tx:      tx,
			Success: false,
			Error:   err,
			TxMeta:  types.NewTxMeta(tx, slot, blockHash, types.TxStatusFailed, err.Error()),
		}
	}

	// Create success result
	txMeta := types.NewTxMeta(tx, slot, blockHash, types.TxStatusSuccess, "")

	// Add to history
	addHistory(state[tx.Sender], tx)
	if tx.Recipient != tx.Sender {
		addHistory(state[tx.Recipient], tx)
	}

	return ExecutionResult{
		Tx:           tx,
		Success:      true,
		Error:        nil,
		UpdatedState: state,
		TxMeta:       txMeta,
	}
}

func (pe *ParallelExecutor) ValidateAndCommit(
	results []ExecutionResult,
	accountStore store.AccountStore,
	txMetaStore store.TxMetaStore,
) error {

	// Validate all results
	for _, result := range results {
		if !result.Success {
			continue
		}

		for addr, acc := range result.UpdatedState {
			if acc.Balance == nil {
				return fmt.Errorf("invalid account state for %s", addr)
			}
		}
	}

	var accountsToStore []*types.Account
	var txMetasToStore []*types.TransactionMeta

	for _, result := range results {
		if !result.Success {
			if result.TxMeta != nil {
				txMetasToStore = append(txMetasToStore, result.TxMeta)
			}
			continue
		}

		// Collect accounts to store
		for _, acc := range result.UpdatedState {
			accountsToStore = append(accountsToStore, acc)
		}

		// Collect transaction metadata
		if result.TxMeta != nil {
			txMetasToStore = append(txMetasToStore, result.TxMeta)
		}
	}

	// Batch store accounts
	if len(accountsToStore) > 0 {
		if err := accountStore.StoreBatch(accountsToStore); err != nil {
			return fmt.Errorf("failed to store accounts: %w", err)
		}
	}

	// Batch store transaction metadata
	if len(txMetasToStore) > 0 {
		if err := txMetaStore.StoreBatch(txMetasToStore); err != nil {
			return fmt.Errorf("failed to store transaction metadata: %w", err)
		}
	}

	return nil
}
