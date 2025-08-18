package staking

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/blockstore"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/p2p"
	"github.com/mezonai/mmn/poh"
)

const NoSlot = ^uint64(0)

// StakeValidator extends the original Validator with PoS capabilities
type StakeValidator struct {
	// Original validator fields
	Pubkey       string
	PrivKey      ed25519.PrivateKey
	Recorder     *poh.PohRecorder
	Service      *poh.PohService
	Mempool      *mempool.Mempool
	TicksPerSlot uint64

	// Configurable parameters
	leaderBatchLoopInterval   time.Duration
	roleMonitorLoopInterval   time.Duration
	leaderTimeout             time.Duration
	leaderTimeoutLoopInterval time.Duration
	BatchSize                 int

	netClient   interfaces.Broadcaster
	blockStore  blockstore.Store
	ledger      *ledger.Ledger
	session     *ledger.Session
	lastSession *ledger.Session
	collector   *consensus.Collector

	// PoS integration
	stakeManager *StakeManager
	
	// Slot & entry buffer
	lastSlot          uint64
	leaderStartAtSlot uint64
	collectedEntries  []poh.Entry
	stopCh            chan struct{}

	// Performance tracking
	slotsProduced     uint64
	blocksProduced    uint64
	slotsMissed       uint64
	lastPerformance   float64
}

// NewStakeValidator creates a new stake-enabled validator
func NewStakeValidator(
	pubkey string,
	privKey ed25519.PrivateKey,
	rec *poh.PohRecorder,
	svc *poh.PohService,
	mempool *mempool.Mempool,
	ticksPerSlot uint64,
	leaderBatchLoopInterval time.Duration,
	roleMonitorLoopInterval time.Duration,
	leaderTimeout time.Duration,
	leaderTimeoutLoopInterval time.Duration,
	batchSize int,
	p2pClient *p2p.Libp2pNetwork,
	blockStore blockstore.Store,
	ledger *ledger.Ledger,
	collector *consensus.Collector,
	stakeManager *StakeManager,
) *StakeValidator {
	v := &StakeValidator{
		Pubkey:                    pubkey,
		PrivKey:                   privKey,
		Recorder:                  rec,
		Service:                   svc,
		Mempool:                   mempool,
		TicksPerSlot:              ticksPerSlot,
		leaderBatchLoopInterval:   leaderBatchLoopInterval,
		roleMonitorLoopInterval:   roleMonitorLoopInterval,
		leaderTimeout:             leaderTimeout,
		leaderTimeoutLoopInterval: leaderTimeoutLoopInterval,
		BatchSize:                 batchSize,
		netClient:                 p2pClient,
		blockStore:                blockStore,
		ledger:                    ledger,
		session:                   ledger.NewSession(),
		lastSession:               ledger.NewSession(),
		lastSlot:                  0,
		leaderStartAtSlot:         NoSlot,
		collectedEntries:          make([]poh.Entry, 0),
		collector:                 collector,
		stakeManager:              stakeManager,
		stopCh:                    make(chan struct{}),
	}
	svc.OnEntry = v.handleEntry
	return v
}

// Run starts the validator
func (v *StakeValidator) Run() {
	go v.leaderBatchLoop()
	go v.roleMonitorLoop()
	go v.leaderTimeoutLoop()
	go v.performanceTracker()
}

// Stop stops the validator
func (v *StakeValidator) Stop() {
	close(v.stopCh)
}

// handleEntry processes entries from PoH service
func (v *StakeValidator) handleEntry(entries []poh.Entry) {
	v.collectedEntries = append(v.collectedEntries, entries...)
	
	currentSlot := v.Recorder.CurrentSlot()
	
	// Use stake manager to check leadership
	if v.stakeManager.IsLeaderForSlot(v.Pubkey, currentSlot) {
		if v.leaderStartAtSlot == NoSlot {
			v.onLeaderSlotStart(currentSlot)
		}
		v.onLeaderSlotTick(currentSlot)
	} else {
		v.onFollowerSlot(currentSlot)
	}
}

// onLeaderSlotStart initializes leader slot
func (v *StakeValidator) onLeaderSlotStart(currentSlot uint64) {
	v.leaderStartAtSlot = currentSlot
	logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Starting leader slot %d", currentSlot))
	
	// Generate stake proof for this slot
	proof, err := v.stakeManager.stakePool.GenerateStakeProof(v.Pubkey, currentSlot, v.PrivKey)
	if err != nil {
		logx.Error("STAKE_VALIDATOR", "Failed to generate stake proof:", err)
		return
	}
	
	logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Generated stake proof for slot %d: %x", currentSlot, proof[:8]))
}

// onLeaderSlotTick processes leader slot tick
func (v *StakeValidator) onLeaderSlotTick(currentSlot uint64) {
	// Collect transactions from mempool
	txs := v.Mempool.PullBatch(v.BatchSize)
	
	// Include staking transactions
	stakeTxs := v.getStakeTransactionsFromMempool()
	if len(stakeTxs) > 0 {
		logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Including %d stake transactions", len(stakeTxs)))
		// Convert stake transactions to regular transactions for inclusion
		for _, stakeTx := range stakeTxs {
			serialized, err := SerializeTransaction(stakeTx)
			if err == nil {
				txs = append(txs, serialized)
			}
		}
	}
	
	if len(txs) > 0 {
		// Record transactions with PoH
		entry, err := v.Recorder.RecordTxs(txs)
		if err == nil {
			v.collectedEntries = append(v.collectedEntries, *entry)
		}
	}
	
	// Check if slot is complete
	if v.isSlotComplete(currentSlot) {
		v.finalizeLeaderSlot(currentSlot)
	}
}

// onFollowerSlot processes follower slot
func (v *StakeValidator) onFollowerSlot(currentSlot uint64) {
	// Wait for block from leader and validate
	// In the current implementation, this is handled by the network layer
	
	// Track slot completion for performance metrics
	if currentSlot > v.lastSlot {
		v.lastSlot = currentSlot
		// Notify stake manager of slot completion
		v.stakeManager.OnSlotComplete(currentSlot, "", true)
	}
}

// finalizeLeaderSlot completes block production for leader slot
func (v *StakeValidator) finalizeLeaderSlot(slot uint64) {
	logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Finalizing leader slot %d with %d entries", 
		slot, len(v.collectedEntries)))
	
	// Create block from collected entries
	block := v.createBlockFromEntries(slot, v.collectedEntries)
	
	// Apply block to ledger session
	session := v.ledger.NewSession()
	err := v.applyBlockToSession(block, session)
	if err != nil {
		logx.Error("STAKE_VALIDATOR", "Failed to apply block to session:", err)
		v.slotsMissed++
		v.resetLeaderSlot()
		return
	}
	
	// Commit session
	v.session = session
	
	// Store block
	err = v.blockStore.AddBlockPending(block)
	if err != nil {
		logx.Error("STAKE_VALIDATOR", "Failed to store block:", err)
		v.slotsMissed++
		v.resetLeaderSlot()
		return
	}
	
	// Broadcast block to network
	ctx := context.Background()
	err = v.netClient.BroadcastBlock(ctx, block)
	if err != nil {
		logx.Error("STAKE_VALIDATOR", "Failed to broadcast block:", err)
	}
	
	// Update performance metrics
	v.blocksProduced++
	v.slotsProduced++
	
	// Notify stake manager of successful slot completion
	v.stakeManager.OnSlotComplete(slot, v.Pubkey, true)
	
	// Reset for next slot
	v.resetLeaderSlot()
	
	logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Successfully produced block for slot %d", slot))
}

// Background loops

func (v *StakeValidator) leaderBatchLoop() {
	ticker := time.NewTicker(v.leaderBatchLoopInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			v.processPendingTransactions()
		}
	}
}

func (v *StakeValidator) roleMonitorLoop() {
	ticker := time.NewTicker(v.roleMonitorLoopInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			v.monitorRole()
		}
	}
}

func (v *StakeValidator) leaderTimeoutLoop() {
	ticker := time.NewTicker(v.leaderTimeoutLoopInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			v.checkLeaderTimeout()
		}
	}
}

func (v *StakeValidator) performanceTracker() {
	ticker := time.NewTicker(30 * time.Second) // Update every 30 seconds
	defer ticker.Stop()
	
	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			v.updatePerformanceMetrics()
		}
	}
}

// Helper methods

func (v *StakeValidator) getStakeTransactionsFromMempool() []*StakeTransaction {
	// In a real implementation, this would extract stake transactions from mempool
	// For now, return empty slice
	return []*StakeTransaction{}
}

func (v *StakeValidator) isSlotComplete(currentSlot uint64) bool {
	// Check if we have enough entries or reached slot boundary
	return len(v.collectedEntries) >= v.BatchSize || 
		   (v.leaderStartAtSlot != NoSlot && currentSlot > v.leaderStartAtSlot)
}

func (v *StakeValidator) createBlockFromEntries(slot uint64, entries []poh.Entry) *block.Block {
	// Use AssembleBlock function to create block properly
	b := block.AssembleBlock(
		slot,
		[32]byte{}, // Should be actual parent hash
		v.Pubkey,
		entries,
	)
	
	// Set status to pending
	b.Status = block.BlockPending
	
	return b
}

func (v *StakeValidator) applyBlockToSession(block *block.Block, session *ledger.Session) error {
	// Apply all transactions in block entries to the session
	for _, entry := range block.Entries {
		if !entry.Tick {
			for _, txData := range entry.Transactions {
				// Check if it's a stake transaction
				if stakeTx, err := DeserializeTransaction(txData); err == nil {
					// Process stake transaction
					err = v.processStakeTransaction(stakeTx, session)
					if err != nil {
						logx.Error("STAKE_VALIDATOR", "Failed to process stake transaction:", err)
						continue
					}
				}
				// Process regular transaction
				// tx, err := types.DeserializeTransaction(txData)
				// if err == nil {
				//     session.ApplyTransaction(tx)
				// }
			}
		}
	}
	return nil
}

func (v *StakeValidator) processStakeTransaction(stakeTx *StakeTransaction, session *ledger.Session) error {
	// Process stake transaction within ledger session
	// This would integrate with the transaction processor
	logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Processing %s transaction", stakeTx.Type.String()))
	return nil
}

func (v *StakeValidator) resetLeaderSlot() {
	v.collectedEntries = make([]poh.Entry, 0)
	v.leaderStartAtSlot = NoSlot
}

func (v *StakeValidator) processPendingTransactions() {
	// Process any pending transactions
	if v.leaderStartAtSlot != NoSlot {
		currentSlot := v.Recorder.CurrentSlot()
		if currentSlot == v.leaderStartAtSlot {
			v.onLeaderSlotTick(currentSlot)
		}
	}
}

func (v *StakeValidator) monitorRole() {
	currentSlot := v.Recorder.CurrentSlot()
	if currentSlot > v.lastSlot {
		v.lastSlot = currentSlot
	}
}

func (v *StakeValidator) checkLeaderTimeout() {
	if v.leaderStartAtSlot == NoSlot {
		return
	}
	
	currentSlot := v.Recorder.CurrentSlot()
	if currentSlot > v.leaderStartAtSlot {
		// Leader slot timeout
		logx.Warn("STAKE_VALIDATOR", fmt.Sprintf("Leader slot %d timeout", v.leaderStartAtSlot))
		v.slotsMissed++
		v.resetLeaderSlot()
	}
}

func (v *StakeValidator) updatePerformanceMetrics() {
	totalSlots := v.slotsProduced + v.slotsMissed
	if totalSlots > 0 {
		v.lastPerformance = float64(v.slotsProduced) / float64(totalSlots)
		logx.Info("STAKE_VALIDATOR", fmt.Sprintf("Performance: %.2f%% (%d/%d slots)", 
			v.lastPerformance*100, v.slotsProduced, totalSlots))
	}
}

// Public API methods

// GetPerformanceMetrics returns validator performance metrics
func (v *StakeValidator) GetPerformanceMetrics() map[string]interface{} {
	return map[string]interface{}{
		"slots_produced":    v.slotsProduced,
		"blocks_produced":   v.blocksProduced,
		"slots_missed":      v.slotsMissed,
		"performance_rate":  v.lastPerformance,
		"is_active":         v.isActiveValidator(),
	}
}

// GetStakeInfo returns staking information for this validator
func (v *StakeValidator) GetStakeInfo() (*ValidatorInfo, bool) {
	return v.stakeManager.GetValidatorInfo(v.Pubkey)
}

// IsLeaderForSlot checks if this validator is leader for a slot
func (v *StakeValidator) IsLeaderForSlot(slot uint64) bool {
	return v.stakeManager.IsLeaderForSlot(v.Pubkey, slot)
}

func (v *StakeValidator) isActiveValidator() bool {
	validators := v.stakeManager.GetActiveValidators()
	_, exists := validators[v.Pubkey]
	return exists
}
