package staking

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/mezonai/mmn/blockstore"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/p2p"
	"github.com/mezonai/mmn/poh"
)

// StakeManager integrates staking with the existing PoH consensus
type StakeManager struct {
	mu sync.RWMutex

	// Core components
	stakePool *StakePool
	scheduler *StakeWeightedScheduler
	processor *StakeTransactionProcessor

	// Blockchain integration
	pohRecorder *poh.PohRecorder
	ledger      *ledger.Ledger
	mempool     *mempool.Mempool
	blockStore  blockstore.Store
	p2pNetwork  *p2p.Libp2pNetwork
	collector   *consensus.Collector

	// Configuration
	slotsPerEpoch     uint64
	minStakeAmount    *big.Int
	maxValidators     int
	epochRewardAmount *big.Int

	// State
	currentEpoch    uint64
	currentSchedule *poh.LeaderSchedule
	epochSeed       []byte

	// Channels
	stakeTxChan chan *StakeTransaction
	stopChan    chan struct{}
}

// StakeManagerConfig holds configuration for the stake manager
type StakeManagerConfig struct {
	SlotsPerEpoch     uint64
	MinStakeAmount    *big.Int
	MaxValidators     int
	EpochRewardAmount *big.Int
}

// NewStakeManager creates a new stake manager
func NewStakeManager(
	config *StakeManagerConfig,
	pohRecorder *poh.PohRecorder,
	ledger *ledger.Ledger,
	mempool *mempool.Mempool,
	blockStore blockstore.Store,
	p2pNetwork *p2p.Libp2pNetwork,
	collector *consensus.Collector,
) *StakeManager {

	stakePool := NewStakePool(config.MinStakeAmount, config.MaxValidators, config.SlotsPerEpoch)
	scheduler := NewStakeWeightedScheduler(stakePool, config.SlotsPerEpoch)
	processor := NewStakeTransactionProcessor(stakePool)

	sm := &StakeManager{
		stakePool:         stakePool,
		scheduler:         scheduler,
		processor:         processor,
		pohRecorder:       pohRecorder,
		ledger:            ledger,
		mempool:           mempool,
		blockStore:        blockStore,
		p2pNetwork:        p2pNetwork,
		collector:         collector,
		slotsPerEpoch:     config.SlotsPerEpoch,
		minStakeAmount:    config.MinStakeAmount,
		maxValidators:     config.MaxValidators,
		epochRewardAmount: config.EpochRewardAmount,
		stakeTxChan:       make(chan *StakeTransaction, 1000),
		stopChan:          make(chan struct{}),
		epochSeed:         []byte("genesis_seed"), // Should be from genesis
	}

	return sm
}

// Start starts the stake manager
func (sm *StakeManager) Start(ctx context.Context) error {
	logx.Info("STAKE_MANAGER", "Starting stake manager...")

	// Initialize first epoch schedule
	if err := sm.initializeEpochSchedule(0); err != nil {
		return fmt.Errorf("failed to initialize epoch schedule: %w", err)
	}

	// Start background processes
	go sm.processStakeTransactions(ctx)
	go sm.epochManager(ctx)

	logx.Info("STAKE_MANAGER", "Stake manager started successfully")
	return nil
}

// Stop stops the stake manager
func (sm *StakeManager) Stop() {
	logx.Info("STAKE_MANAGER", "Stopping stake manager...")
	close(sm.stopChan)
}

// RegisterValidator registers a new validator (used during genesis)
func (sm *StakeManager) RegisterValidator(pubkey string, stakeAmount *big.Int) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	return sm.stakePool.RegisterValidator(pubkey, stakeAmount)
}

// RegisterGenesisValidator registers a genesis validator that activates immediately
func (sm *StakeManager) RegisterGenesisValidator(pubkey string, stakeAmount *big.Int) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	return sm.stakePool.RegisterGenesisValidator(pubkey, stakeAmount)
}

// SubmitStakeTransaction submits a staking transaction to the mempool
func (sm *StakeManager) SubmitStakeTransaction(tx *StakeTransaction) error {
	select {
	case sm.stakeTxChan <- tx:
		return nil
	default:
		return fmt.Errorf("stake transaction channel full")
	}
}

// GetCurrentLeaderSchedule returns the current leader schedule
func (sm *StakeManager) GetCurrentLeaderSchedule() *poh.LeaderSchedule {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.currentSchedule
}

// GetValidatorInfo returns information about a validator
func (sm *StakeManager) GetValidatorInfo(pubkey string) (*ValidatorInfo, bool) {
	return sm.stakePool.GetValidatorInfo(pubkey)
}

// GetActiveValidators returns all active validators
func (sm *StakeManager) GetActiveValidators() map[string]*ValidatorInfo {
	return sm.stakePool.GetActiveValidators()
}

// GetStakeDistribution returns current stake distribution
func (sm *StakeManager) GetStakeDistribution() map[string]float64 {
	return sm.stakePool.GetStakeDistribution()
}

// IsLeaderForSlot checks if a validator is leader for a specific slot
func (sm *StakeManager) IsLeaderForSlot(validatorPubkey string, slot uint64) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.currentSchedule == nil {
		return false
	}

	leader, exists := sm.currentSchedule.LeaderAt(slot)
	return exists && leader == validatorPubkey
}

// OnSlotComplete is called when a slot is completed
func (sm *StakeManager) OnSlotComplete(slot uint64, leader string, success bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if we need to transition to new epoch
	newEpoch := slot / sm.slotsPerEpoch
	if newEpoch > sm.currentEpoch {
		sm.transitionToNewEpoch(newEpoch, slot)
	}
}

// Private methods

func (sm *StakeManager) initializeEpochSchedule(epoch uint64) error {
	schedule, err := sm.scheduler.GenerateLeaderSchedule(epoch, sm.epochSeed)
	if err != nil {
		return err
	}

	sm.currentSchedule = schedule
	sm.currentEpoch = epoch
	sm.stakePool.UpdateEpoch(epoch)

	logx.Info("STAKE_MANAGER", fmt.Sprintf("Initialized epoch %d schedule", epoch))
	return nil
}

func (sm *StakeManager) transitionToNewEpoch(newEpoch, currentSlot uint64) {
	logx.Info("STAKE_MANAGER", fmt.Sprintf("Transitioning to epoch %d at slot %d", newEpoch, currentSlot))

	// Distribute rewards for completed epoch
	sm.distributeEpochRewards(sm.currentEpoch, currentSlot-1)

	// Generate new schedule
	newSeed := sm.generateEpochSeed(newEpoch)
	schedule, err := sm.scheduler.GenerateLeaderSchedule(newEpoch, newSeed)
	if err != nil {
		logx.Error("STAKE_MANAGER", "Failed to generate new epoch schedule:", err)
		return
	}

	// Update state
	sm.currentSchedule = schedule
	sm.currentEpoch = newEpoch
	sm.epochSeed = newSeed
	sm.stakePool.UpdateEpoch(newEpoch)

	logx.Info("STAKE_MANAGER", fmt.Sprintf("Epoch %d transition complete", newEpoch))
}

func (sm *StakeManager) distributeEpochRewards(epoch, finalSlot uint64) {
	if sm.epochRewardAmount.Sign() <= 0 {
		return
	}

	logx.Info("STAKE_MANAGER", fmt.Sprintf("Distributing rewards for epoch %d", epoch))

	rewards := sm.stakePool.DistributeRewards(sm.epochRewardAmount, finalSlot)

	// Apply rewards to ledger
	for pubkey, amount := range rewards {
		if amount.Sign() > 0 {
			// In a real implementation, this would update account balances
			logx.Info("STAKE_MANAGER", fmt.Sprintf("Rewarded %s: %s tokens", pubkey, amount.String()))
		}
	}
}

func (sm *StakeManager) generateEpochSeed(epoch uint64) []byte {
	// In a real implementation, this would use the hash of the last block
	// of the previous epoch combined with some randomness
	seed := fmt.Sprintf("epoch_%d_seed", epoch)
	return []byte(seed)
}

func (sm *StakeManager) processStakeTransactions(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sm.stopChan:
			return
		case tx := <-sm.stakeTxChan:
			sm.handleStakeTransaction(tx)
		case <-ticker.C:
			// Periodic processing of pending transactions
			sm.processPendingStakeTransactions()
		}
	}
}

func (sm *StakeManager) handleStakeTransaction(tx *StakeTransaction) {
	// In a real implementation, this would verify the transaction
	// and add it to the mempool for inclusion in blocks
	logx.Info("STAKE_MANAGER", fmt.Sprintf("Received %s transaction from %s",
		tx.Type.String(), tx.DelegatorPubkey))

	// For now, process immediately (in production, this would be in a block)
	// err := sm.processor.ProcessTransaction(tx, senderPubKey)
	// if err != nil {
	//     logx.Error("STAKE_MANAGER", "Failed to process stake transaction:", err)
	// }
}

func (sm *StakeManager) processPendingStakeTransactions() {
	// Process any pending stake transactions from blocks
	// This would integrate with the block processing pipeline
}

func (sm *StakeManager) epochManager(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sm.stopChan:
			return
		case <-ticker.C:
			// Monitor epoch transitions
			currentSlot := sm.getCurrentSlot()
			expectedEpoch := currentSlot / sm.slotsPerEpoch

			if expectedEpoch > sm.currentEpoch {
				sm.mu.Lock()
				if expectedEpoch > sm.currentEpoch {
					sm.transitionToNewEpoch(expectedEpoch, currentSlot)
				}
				sm.mu.Unlock()
			}
		}
	}
}

func (sm *StakeManager) getCurrentSlot() uint64 {
	// This should return the current blockchain slot
	// For now, return a placeholder based on time
	return uint64(time.Now().Unix() / 400) // Assuming 400ms slots
}

// API methods for external interaction

// GetStakePoolStats returns statistics about the stake pool
func (sm *StakeManager) GetStakePoolStats() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	activeValidators := sm.stakePool.GetActiveValidators()
	totalStake := sm.stakePool.GetTotalStake()

	return map[string]interface{}{
		"total_validators":  len(sm.stakePool.validators),
		"active_validators": len(activeValidators),
		"total_stake":       totalStake.String(),
		"current_epoch":     sm.currentEpoch,
		"slots_per_epoch":   sm.slotsPerEpoch,
		"min_stake_amount":  sm.minStakeAmount.String(),
		"max_validators":    sm.maxValidators,
	}
}

// GetEpochInfo returns information about the current epoch
func (sm *StakeManager) GetEpochInfo() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	currentSlot := sm.getCurrentSlot()
	epochStartSlot := sm.currentEpoch * sm.slotsPerEpoch
	epochEndSlot := (sm.currentEpoch+1)*sm.slotsPerEpoch - 1

	return map[string]interface{}{
		"current_epoch":    sm.currentEpoch,
		"current_slot":     currentSlot,
		"epoch_start_slot": epochStartSlot,
		"epoch_end_slot":   epochEndSlot,
		"slots_remaining":  epochEndSlot - currentSlot,
		"epoch_progress":   float64(currentSlot-epochStartSlot) / float64(sm.slotsPerEpoch),
	}
}
