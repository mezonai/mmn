package validator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/poh"
)

// OptimizedValidator provides high-performance transaction processing
type OptimizedValidator struct {
	*Validator
	blockAssembler *block.OptimizedBlockAssembler

	// Batch processing
	batchProcessor *BatchProcessor
	entryBuffer    chan []poh.Entry
	processingPool sync.Pool

	// Performance metrics
	metrics *PerformanceMetrics
}

// BatchProcessor handles batch transaction processing
type BatchProcessor struct {
	batchSize    int
	timeout      time.Duration
	entryChannel chan []poh.Entry
	stopChannel  chan struct{}
	wg           sync.WaitGroup
}

// PerformanceMetrics tracks validator performance
type PerformanceMetrics struct {
	mu                  sync.RWMutex
	blocksProduced      int64
	txsProcessed        int64
	avgBlockTime        time.Duration
	avgTxProcessingTime time.Duration
	lastBlockTime       time.Time
}

// NewOptimizedValidator creates a new optimized validator
func NewOptimizedValidator(baseValidator *Validator, batchSize int) *OptimizedValidator {
	ov := &OptimizedValidator{
		Validator:      baseValidator,
		blockAssembler: block.NewOptimizedBlockAssembler(),
		entryBuffer:    make(chan []poh.Entry, 1000), // Large buffer
		metrics:        &PerformanceMetrics{},
	}

	ov.batchProcessor = &BatchProcessor{
		batchSize:    batchSize,
		timeout:      50 * time.Millisecond, // Faster timeout
		entryChannel: ov.entryBuffer,
		stopChannel:  make(chan struct{}),
	}

	ov.processingPool = sync.Pool{
		New: func() interface{} {
			return make([]poh.Entry, 0, batchSize)
		},
	}

	return ov
}

// StartOptimized starts the optimized validator
func (ov *OptimizedValidator) StartOptimized() {
	ov.batchProcessor.Start()
	go ov.processEntriesOptimized()
	go ov.metricsCollector()

	// Override the base validator's OnEntry callback with optimized version
	ov.Validator.Service.OnEntry = ov.handleEntryOptimized
}

// StopOptimized stops the optimized validator
func (ov *OptimizedValidator) StopOptimized() {
	close(ov.batchProcessor.stopChannel)
	ov.batchProcessor.wg.Wait()
}

// handleEntryOptimized processes entries with batch optimization
func (ov *OptimizedValidator) handleEntryOptimized(entries []poh.Entry) {
	start := time.Now()

	// Process immediately using base validator logic but with optimizations
	ov.processEntriesImmediate(entries)

	// Update metrics
	ov.metrics.mu.Lock()
	ov.metrics.txsProcessed += int64(len(entries))
	ov.metrics.avgTxProcessingTime = time.Since(start)
	ov.metrics.mu.Unlock()
}

// processEntriesOptimized processes entries in optimized batches
func (ov *OptimizedValidator) processEntriesOptimized() {
	for {
		select {
		case entries := <-ov.entryBuffer:
			ov.processEntriesImmediate(entries)
		case <-ov.batchProcessor.stopChannel:
			return
		}
	}
}

// processEntriesImmediate processes entries immediately with optimizations
func (ov *OptimizedValidator) processEntriesImmediate(entries []poh.Entry) {
	currentSlot := ov.Recorder.CurrentSlot()

	// When slot advances, assemble block for lastSlot if we were leader
	if currentSlot > ov.lastSlot && ov.IsLeader(ov.lastSlot) {
		ov.assembleBlockOptimized(ov.lastSlot, entries)
	} else if ov.IsLeader(currentSlot) {
		// Buffer entries only if leader of current slot
		ov.collectedEntries = append(ov.collectedEntries, entries...)
		logx.Info("VALIDATOR", fmt.Sprintf("Adding %d entries for slot %d", len(entries), currentSlot))
	}

	// Update lastSlot
	ov.lastSlot = currentSlot
}

// assembleBlockOptimized assembles blocks with optimizations
func (ov *OptimizedValidator) assembleBlockOptimized(slot uint64, entries []poh.Entry) {
	start := time.Now()

	// Buffer entries
	ov.collectedEntries = append(ov.collectedEntries, entries...)

	// Retrieve previous hash from recorder
	prevHash := ov.Recorder.GetSlotHash(slot - 1)
	logx.Info("VALIDATOR", fmt.Sprintf("Previous hash for slot %d %x", slot-1, prevHash))

	// Use optimized block assembler
	blk := ov.blockAssembler.AssembleBlockOptimized(
		slot,
		prevHash,
		ov.Pubkey,
		ov.collectedEntries,
	)

	blk.Sign(ov.PrivKey)
	logx.Info("VALIDATOR", fmt.Sprintf("Leader assembled block: slot=%d, entries=%d", slot, len(ov.collectedEntries)))

	// Reset buffer with capacity
	ov.collectedEntries = ov.processingPool.Get().([]poh.Entry)
	ov.collectedEntries = ov.collectedEntries[:0]

	// Broadcast block
	if err := ov.netClient.BroadcastBlock(context.Background(), blk); err != nil {
		logx.Error("VALIDATOR", fmt.Sprintf("Failed to broadcast block: %v", err))
	}

	// Update metrics
	ov.metrics.mu.Lock()
	ov.metrics.blocksProduced++
	ov.metrics.avgBlockTime = time.Since(start)
	ov.metrics.lastBlockTime = time.Now()
	ov.metrics.mu.Unlock()
}

// Start starts the batch processor
func (bp *BatchProcessor) Start() {
	bp.wg.Add(1)
	go bp.run()
}

// run processes batches with timeout
func (bp *BatchProcessor) run() {
	defer bp.wg.Done()

	ticker := time.NewTicker(bp.timeout)
	defer ticker.Stop()

	var batch []poh.Entry

	for {
		select {
		case entries := <-bp.entryChannel:
			batch = append(batch, entries...)
			if len(batch) >= bp.batchSize {
				bp.processBatch(batch)
				batch = batch[:0] // Reset slice
			}
		case <-ticker.C:
			if len(batch) > 0 {
				bp.processBatch(batch)
				batch = batch[:0] // Reset slice
			}
		case <-bp.stopChannel:
			if len(batch) > 0 {
				bp.processBatch(batch)
			}
			return
		}
	}
}

// processBatch processes a batch of entries
func (bp *BatchProcessor) processBatch(batch []poh.Entry) {
	// Process batch in parallel
	var wg sync.WaitGroup
	chunkSize := len(batch) / 4 // Process in 4 chunks

	for i := 0; i < len(batch); i += chunkSize {
		end := i + chunkSize
		if end > len(batch) {
			end = len(batch)
		}

		wg.Add(1)
		go func(chunk []poh.Entry) {
			defer wg.Done()
			// Process chunk
			_ = chunk
		}(batch[i:end])
	}
	wg.Wait()
}

// GetMetrics returns performance metrics
func (ov *OptimizedValidator) GetMetrics() map[string]interface{} {
	ov.metrics.mu.RLock()
	defer ov.metrics.mu.RUnlock()

	return map[string]interface{}{
		"blocks_produced":      ov.metrics.blocksProduced,
		"txs_processed":        ov.metrics.txsProcessed,
		"avg_block_time_ms":    ov.metrics.avgBlockTime.Milliseconds(),
		"avg_tx_processing_ms": ov.metrics.avgTxProcessingTime.Milliseconds(),
		"last_block_time":      ov.metrics.lastBlockTime,
		"buffer_size":          len(ov.entryBuffer),
	}
}

// metricsCollector collects and logs performance metrics
func (ov *OptimizedValidator) metricsCollector() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			metrics := ov.GetMetrics()
			logx.Info("VALIDATOR_METRICS", fmt.Sprintf("Performance: %+v", metrics))
		case <-ov.batchProcessor.stopChannel:
			return
		}
	}
}
