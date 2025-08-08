package events

import (
	"mmn/types"
	"sync"
)

// EventType định nghĩa các loại event
type EventType string

const (
	TxSubmittedEvent  EventType = "tx_submitted"
	TxConfirmedEvent  EventType = "tx_confirmed"
	TxFailedEvent     EventType = "tx_failed"
	BlockCreatedEvent EventType = "block_created"
	MempoolStatsEvent EventType = "mempool_stats"
)

// Event data structures
type TxSubmittedData struct {
	TxHash string
	Tx     *types.Transaction
}

type TxConfirmedData struct {
	TxHash string
	Tx     *types.Transaction
	Slot   uint64
}

type TxFailedData struct {
	TxHash string
	Tx     *types.Transaction
	Error  string
}

type BlockCreatedData struct {
	Slot     uint64
	TxHashes []string
	TxCount  int
}

type MempoolStatsData struct {
	TxCount  int
	TxHashes []string
	MaxSize  int
	IsFull   bool
}

// EventListener function type
type EventListener func(eventType EventType, data interface{})

// EventBus manages event publishing and subscription
type EventBus struct {
	listeners map[EventType][]EventListener
	mu        sync.RWMutex
}

// NewEventBus creates a new event bus
func NewEventBus() *EventBus {
	return &EventBus{
		listeners: make(map[EventType][]EventListener),
	}
}

// Subscribe adds a listener for a specific event type
func (eb *EventBus) Subscribe(eventType EventType, listener EventListener) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.listeners[eventType] = append(eb.listeners[eventType], listener)
}

// Publish sends an event to all subscribed listeners
func (eb *EventBus) Publish(eventType EventType, data interface{}) {
	eb.mu.RLock()
	listeners := eb.listeners[eventType]
	eb.mu.RUnlock()

	// Execute listeners in goroutines to avoid blocking
	for _, listener := range listeners {
		go listener(eventType, data)
	}
}

// PublishTxSubmitted publishes a transaction submitted event
func (eb *EventBus) PublishTxSubmitted(txHash string, tx *types.Transaction) {
	eb.Publish(TxSubmittedEvent, TxSubmittedData{
		TxHash: txHash,
		Tx:     tx,
	})
}

// PublishTxConfirmed publishes a transaction confirmed event
func (eb *EventBus) PublishTxConfirmed(txHash string, tx *types.Transaction, slot uint64) {
	eb.Publish(TxConfirmedEvent, TxConfirmedData{
		TxHash: txHash,
		Tx:     tx,
		Slot:   slot,
	})
}

// PublishTxFailed publishes a transaction failed event
func (eb *EventBus) PublishTxFailed(txHash string, tx *types.Transaction, errorMsg string) {
	eb.Publish(TxFailedEvent, TxFailedData{
		TxHash: txHash,
		Tx:     tx,
		Error:  errorMsg,
	})
}

// PublishBlockCreated publishes a block created event
func (eb *EventBus) PublishBlockCreated(slot uint64, txHashes []string) {
	eb.Publish(BlockCreatedEvent, BlockCreatedData{
		Slot:     slot,
		TxHashes: txHashes,
		TxCount:  len(txHashes),
	})
}

// PublishMempoolStats publishes mempool statistics
func (eb *EventBus) PublishMempoolStats(txCount int, txHashes []string, maxSize int) {
	eb.Publish(MempoolStatsEvent, MempoolStatsData{
		TxCount:  txCount,
		TxHashes: txHashes,
		MaxSize:  maxSize,
		IsFull:   txCount >= maxSize,
	})
}
