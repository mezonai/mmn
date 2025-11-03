package validator

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/mezonai/mmn/alpenglow/pool"
	"github.com/mezonai/mmn/exception"

	"github.com/mezonai/mmn/store"

	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/utils"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/p2p"
	"github.com/mezonai/mmn/poh"
)

const (
	TIMEOUT_FOR_SLOT = 300 * time.Millisecond
)

// Validator encapsulates leader/follower behavior.
type Validator struct {
	Pubkey   string
	PrivKey  ed25519.PrivateKey
	Recorder *poh.PohRecorder
	Service  *poh.PohService
	Schedule *poh.LeaderSchedule
	Mempool  *mempool.Mempool

	// Configurable parameters
	leaderBatchLoopInterval   time.Duration
	roleMonitorLoopInterval   time.Duration
	leaderTimeout             time.Duration
	leaderTimeoutLoopInterval time.Duration
	BatchSize                 int

	p2pClient  *p2p.Libp2pNetwork
	blockStore store.BlockStore
	// Slot & entry buffer
	pendingTxs    []*transaction.Transaction
	pool          *pool.Pool
	slotCh        <-chan uint64
	timeoutSlotCh chan struct{}
	stopCh        chan struct{}

	currentSlot     uint64
	prevBlockHash   [32]byte
	isWindowSkipped bool
	// Additional dependencies for block processing
	onBroadcastBlock func(ctx context.Context, blk *block.BroadcastedBlock) error
}

// NewValidator constructs a Validator with dependencies, including blockStore.
func NewValidator(
	pubkey string,
	privKey ed25519.PrivateKey,
	rec *poh.PohRecorder,
	svc *poh.PohService,
	schedule *poh.LeaderSchedule,
	mempool *mempool.Mempool,
	leaderBatchLoopInterval time.Duration,
	roleMonitorLoopInterval time.Duration,
	leaderTimeout time.Duration,
	leaderTimeoutLoopInterval time.Duration,
	batchSize int,
	p2pClient *p2p.Libp2pNetwork,
	blockStore store.BlockStore,
	pool *pool.Pool,
	slotCh <-chan uint64,
) *Validator {
	v := &Validator{
		Pubkey:                    pubkey,
		PrivKey:                   privKey,
		Recorder:                  rec,
		Service:                   svc,
		Schedule:                  schedule,
		Mempool:                   mempool,
		leaderBatchLoopInterval:   leaderBatchLoopInterval,
		roleMonitorLoopInterval:   roleMonitorLoopInterval,
		leaderTimeout:             leaderTimeout,
		leaderTimeoutLoopInterval: leaderTimeoutLoopInterval,
		p2pClient:                 p2pClient,
		blockStore:                blockStore,
		pendingTxs:                make([]*transaction.Transaction, 0, batchSize),
		pool:                      pool,
		slotCh:                    slotCh,
		timeoutSlotCh:             make(chan struct{}, 1),
		currentSlot:               0,
		prevBlockHash:             [32]byte{},
		isWindowSkipped:           false,
	}

	p2pClient.OnGetLatestSlot = v.getCurrentSlot
	v.onBroadcastBlock = p2pClient.BroadcastBlockWithProcessing
	return v
}

func (v *Validator) onLeaderSlotStart(currentSlot uint64) {
	logx.Info("LEADER", "onLeaderSlotStart", currentSlot)
	ticker := time.NewTicker(v.leaderTimeoutLoopInterval)
	deadline := time.NewTimer(v.leaderTimeout)
	defer ticker.Stop()
	defer deadline.Stop()

waitLoop:
	for {
		select {
		case <-ticker.C:
			if currentSlot == 1 {
				logx.Info("LEADER", "Genesis block, using genesis hash as seed")
				v.prevBlockHash = v.blockStore.Block(0).Hash
				break waitLoop
			}

			parentId := v.pool.GetParentsReady(currentSlot)
			if parentId != (pool.BlockId{}) {
				logx.Info("LEADER", fmt.Sprintf("Found parent ready for slot %d: %x", currentSlot, hex.EncodeToString(parentId.BlockHash[:])))
				v.prevBlockHash = parentId.BlockHash
				v.isWindowSkipped = false
				break waitLoop
			} else if v.pool.IsSlotSkipped(currentSlot) {
				logx.Info("LEADER", fmt.Sprintf("Slot %d is skipped, using previous block hash %x as seed", currentSlot, v.prevBlockHash))
				v.isWindowSkipped = true
				break waitLoop
			} else {
				logx.Info("LEADER", fmt.Sprintf("No parent ready for slot %d yet, waiting...", currentSlot))
			}

		case <-deadline.C:
			logx.Warn("LEADER", fmt.Sprintf("Meet at deadline %d", currentSlot))
			lastSeenSlot := v.blockStore.GetLatestStoreSlot()
			v.prevBlockHash = v.blockStore.Block(lastSeenSlot).Hash
			break waitLoop

		case <-v.stopCh:
			return

		}
	}

	v.pendingTxs = make([]*transaction.Transaction, 0, v.BatchSize)
	logx.Info("LEADER", fmt.Sprintf("Leader ready to start at slot: %d", currentSlot))
}

// IsLeader checks if this validator is leader for given slot.
func (v *Validator) IsLeader(slot uint64) bool {
	leader, has := v.Schedule.LeaderAt(slot)
	return has && leader == v.Pubkey
}

func (v *Validator) IsFollower(slot uint64) bool {
	return !v.IsLeader(slot)
}

// handleCreateBlock creates and broadcasts a block from given entries.
func (v *Validator) handleCreateBlock(entries []poh.Entry) {
	if len(entries) != 0 {
		logx.Info("VALIDATOR", fmt.Sprintf("Creating block in slot %d with %d entries", v.currentSlot, len(entries)))

		blk := block.AssembleBlock(
			v.currentSlot,
			v.prevBlockHash,
			v.Pubkey,
			entries,
		)
		blk.Sign(v.PrivKey)
		v.prevBlockHash = blk.Hash
		logx.Info("VALIDATOR", fmt.Sprintf("Created block: slot=%d, entries=%d", v.currentSlot, len(entries)))

		exception.SafeGo("onBroadcastBlock", func() {
			if err := v.onBroadcastBlock(context.Background(), blk); err != nil {
				logx.Error("VALIDATOR", fmt.Sprintf("Failed to process block before broadcast: %v", err))
			}
		})

	} else {
		logx.Warn("VALIDATOR", fmt.Sprintf("No entries for slot %d (skip assembling block)", v.currentSlot))
	}
}

func (v *Validator) peekPendingTxs(size int) []*transaction.Transaction {
	if len(v.pendingTxs) == 0 {
		return nil
	}
	if len(v.pendingTxs) < size {
		size = len(v.pendingTxs)
	}

	result := make([]*transaction.Transaction, size)
	copy(result, v.pendingTxs[:size])

	return result
}

func (v *Validator) dropPendingTxs(size int) {
	if size >= len(v.pendingTxs) {
		v.pendingTxs = v.pendingTxs[:0]
		return
	}

	copy(v.pendingTxs, v.pendingTxs[size:])
	v.pendingTxs = v.pendingTxs[:len(v.pendingTxs)-size]
}

func (v *Validator) Run() {
	v.stopCh = make(chan struct{})

	exception.SafeGoWithPanic("roleMonitorLoop", func() {
		v.roleMonitorLoop()
	})
}

func (v *Validator) roleMonitorLoop() {
	stopBatchLoopCh := make(chan struct{})

	for {
		select {
		case slot := <-v.slotCh:
			logx.Info("VALIDATOR", fmt.Sprintf("Slot %d are ready!", slot))
			v.currentSlot = slot
			if v.IsLeader(slot) {
				v.Recorder.DrainEntries() // Reset entries
				v.setTimeout()

				if utils.IsSlotStartOfWindow(slot) {
					v.onLeaderSlotStart(slot)
				}

				if v.isWindowSkipped {
					continue
				}

				go v.leaderBatchLoop(slot, stopBatchLoopCh)
			} else {

			}

		case <-v.timeoutSlotCh:
			logx.Info("VALIDATOR", fmt.Sprintf("Timeout for slot %d", v.currentSlot))
			stopBatchLoopCh <- struct{}{}
			v.handleCreateBlock(v.Recorder.DrainEntries())

		case <-v.stopCh:
			return
		}
	}
}

func (v *Validator) leaderBatchLoop(slot uint64, stopCh chan struct{}) {
	batchTicker := time.NewTicker(v.leaderBatchLoopInterval)
	defer batchTicker.Stop()
	for {
		select {
		case <-stopCh:
			logx.Info("LEADER", fmt.Sprintf("Stopping batch loop for slot %d", slot))
			return
		case <-batchTicker.C:

			logx.Info("LEADER", fmt.Sprintf("Pulling batch for slot %d", slot))
			batch := v.Mempool.PullBatch(v.BatchSize)
			if len(batch) == 0 && len(v.pendingTxs) == 0 {
				logx.Debug("LEADER", fmt.Sprintf("No batch for slot %d", slot))
				continue
			}

			for _, r := range batch {
				tx, err := utils.ParseTx(r)
				if err != nil {
					logx.Error("LEADER", fmt.Sprintf("Failed to parse transaction: %v", err))
					continue
				}
				v.pendingTxs = append(v.pendingTxs, tx)
			}

			recordTxs := v.peekPendingTxs(v.BatchSize)
			if recordTxs == nil {
				logx.Debug("LEADER", fmt.Sprintf("No valid transactions for slot %d", slot))
				continue
			}
			logx.Info("LEADER", fmt.Sprintf("Recording batch for slot %d", slot))
			entry, err := v.Recorder.RecordTxs(recordTxs)
			if err != nil {
				logx.Warn("LEADER", fmt.Sprintf("Record error: %v", err))
				continue
			}
			v.dropPendingTxs(len(recordTxs))
			logx.Info("LEADER", fmt.Sprintf("Recorded %d tx (slot=%d, entry=%x...)", len(recordTxs), slot, entry.Hash[:6]))
		}
	}
}

func (v *Validator) setTimeout() {
	sender := v.timeoutSlotCh

	go func() {
		time.Sleep(TIMEOUT_FOR_SLOT)
		logx.Info("VALIDATOR", "Timeout triggered, sending signal")
		sender <- struct{}{}
	}()
}

func (v *Validator) getCurrentSlot() uint64 {
	return v.currentSlot
}

// func (v *Validator) mempoolCleanupLoop() {
// 	cleanupTicker := time.NewTicker(1 * time.Minute)
// 	defer cleanupTicker.Stop()

// 	for {
// 		select {
// 		case <-v.stopCh:
// 			return
// 		case <-cleanupTicker.C:
// 			logx.Info("VALIDATOR", "Running periodic mempool cleanup")
// 			v.Mempool.PeriodicCleanup()
// 		}
// 	}
// }
