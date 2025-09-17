package ledger

import (
	"context"
	"fmt"
	"sort"
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

	// Per-account locks to serialize execution for transactions touching the same account
	accountLocks   map[string]*sync.Mutex
	accountLocksMu sync.Mutex
}

func NewParallelExecutor() *ParallelExecutor {
	// Default fixed worker count for predictable performance
	defaultWorkers := 32
	if defaultWorkers < 1 {
		defaultWorkers = 1
	}

	return &ParallelExecutor{
		workerCount:  defaultWorkers,
		results:      make(chan ExecutionResult, 1000),
		errors:       make(chan error, 1000),
		accountLocks: make(map[string]*sync.Mutex),
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
	graph := make(map[int][]int, len(dependencies))
	lastWriter := make(map[string]int)

	for i := range dependencies {
		graph[i] = []int{}
	}

	for i, dep := range dependencies {
		for _, acc := range dep.WriteAccounts {
			if prev, ok := lastWriter[acc]; ok {
				graph[i] = append(graph[i], prev)
			}
			lastWriter[acc] = i
		}
	}

	return graph
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

// getOrderedAccountLocks returns per-account mutexes for the provided addresses.
// It ensures deterministic ordering by sorting addresses to avoid deadlocks
// when multiple accounts are locked together.
func (pe *ParallelExecutor) getOrderedAccountLocks(addresses []string) []*sync.Mutex {
	// deduplicate addresses
	uniq := make(map[string]struct{}, len(addresses))
	for _, a := range addresses {
		if a == "" {
			continue
		}
		uniq[a] = struct{}{}
	}

	ordered := make([]string, 0, len(uniq))
	for a := range uniq {
		ordered = append(ordered, a)
	}
	sort.Strings(ordered)

	locks := make([]*sync.Mutex, 0, len(ordered))
	pe.accountLocksMu.Lock()
	for _, a := range ordered {
		lk, ok := pe.accountLocks[a]
		if !ok {
			lk = &sync.Mutex{}
			pe.accountLocks[a] = lk
		}
		locks = append(locks, lk)
	}
	pe.accountLocksMu.Unlock()
	return locks
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

	uniqueRead := make(map[string]struct{})
	for _, idx := range group {
		for _, addr := range dependencies[idx].ReadAccounts {
			if addr == "" {
				continue
			}
			uniqueRead[addr] = struct{}{}
		}
	}
	prefetchList := make([]string, 0, len(uniqueRead))
	for addr := range uniqueRead {
		prefetchList = append(prefetchList, addr)
	}
	prefetchMap, _ := accountStore.GetBatch(prefetchList)

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

			// Execute transaction with prefetch map available
			result := pe.executeTransaction(
				dependencies[txIndex],
				accountStore,
				eventRouter,
				slot,
				blockHash,
				prefetchMap,
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
	prefetch map[string]*types.Account,
) ExecutionResult {

	tx := dep.Tx
	state := make(map[string]*types.Account)

	// Serialize execution for transactions impacting the same accounts
	// Lock both sender and recipient (write set) in deterministic order
	accLocks := pe.getOrderedAccountLocks(dep.WriteAccounts)
	for _, lk := range accLocks {
		lk.Lock()
	}
	defer func() {
		// unlock in reverse order as a good practice
		for i := len(accLocks) - 1; i >= 0; i-- {
			accLocks[i].Unlock()
		}
	}()

	// Load account states (prefetched at group level in executeGroupPrefetch)
	for _, addr := range dep.ReadAccounts {
		acc, ok := state[addr]
		if !ok || acc == nil {
			if prefetch != nil {
				if pf, ok2 := prefetch[addr]; ok2 && pf != nil {
					state[addr] = pf
					continue
				}
			}
			// fallback fetch individually if not present
			dbAcc, err := accountStore.GetByAddr(addr)
			if err != nil {
				return ExecutionResult{Tx: tx, Success: false, Error: fmt.Errorf("failed to load account %s: %w", addr, err)}
			}
			if dbAcc == nil {
				dbAcc = &types.Account{Address: addr, Balance: uint256.NewInt(0), Nonce: 0}
			}
			state[addr] = dbAcc
		}
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
	accountSeen := make(map[string]struct{})
	var txMetasToStore []*types.TransactionMeta

	for _, result := range results {
		if !result.Success {
			if result.TxMeta != nil {
				txMetasToStore = append(txMetasToStore, result.TxMeta)
			}
			continue
		}

		// Collect accounts to store
		for addr, acc := range result.UpdatedState {
			if _, ok := accountSeen[addr]; ok {
				continue
			}
			accountSeen[addr] = struct{}{}
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
