package events

import (
	"fmt"
	"sync"
	"time"
)

// BlockStore interface for getting transaction hashes
type BlockStore interface {
	GetTransactionHashes(slot uint64) []string
}

// EventRouter handles complex event routing logic
type EventRouter struct {
	eventBus   *EventBus
	blockStore BlockStore
	mu         sync.RWMutex
}

// NewEventRouter creates a new EventRouter instance
func NewEventRouter(eventBus *EventBus, blockStore BlockStore) *EventRouter {
	return &EventRouter{
		eventBus:   eventBus,
		blockStore: blockStore,
	}
}

// PublishBlockFinalized publishes a block finalization event to relevant transaction subscribers
func (er *EventRouter) PublishBlockFinalized(slot uint64, blockHash string) {
	er.mu.RLock()
	defer er.mu.RUnlock()

	// Create minimal block event (no transaction hashes)
	event := NewBlockFinalized(slot, blockHash)
	
	// Get transaction hashes from BlockStore (single source of truth)
	txHashes := er.blockStore.GetTransactionHashes(slot)
	
	fmt.Printf("EventRouter: Publishing BlockFinalized for slot %d with %d transactions\n", slot, len(txHashes))
	
	// Use EventBus.Publish to handle both specific and all-events subscribers
	// This avoids duplicate messages since EventBus.Publish handles routing correctly
	er.eventBus.Publish(event)
	
	fmt.Printf("EventRouter: BlockFinalized for slot %d: published to all subscribers\n", slot)
}

// PublishBlockFinalizedWithTimestamp publishes a block finalization event with a specific timestamp
func (er *EventRouter) PublishBlockFinalizedWithTimestamp(slot uint64, blockHash string, timestamp time.Time) {
	er.mu.RLock()
	defer er.mu.RUnlock()

	// Create block event with specific timestamp
	event := NewBlockFinalizedWithTimestamp(slot, blockHash, timestamp)
	
	// Get transaction hashes from BlockStore (single source of truth)
	txHashes := er.blockStore.GetTransactionHashes(slot)
	
	fmt.Printf("EventRouter: Publishing BlockFinalized for slot %d with %d transactions\n", slot, len(txHashes))
	
	// Use EventBus.Publish to handle both specific and all-events subscribers
	// This avoids duplicate messages since EventBus.Publish handles routing correctly
	er.eventBus.Publish(event)
	
	fmt.Printf("EventRouter: BlockFinalized for slot %d: published to all subscribers\n", slot)
}

// PublishTransactionEvent publishes a transaction-specific event
func (er *EventRouter) PublishTransactionEvent(event BlockchainEvent) {
	er.eventBus.Publish(event)
}

// Subscribe subscribes to events for a specific transaction hash
func (er *EventRouter) Subscribe(txHash string) chan BlockchainEvent {
	return er.eventBus.Subscribe(txHash)
}

// SubscribeToAllEvents subscribes to all transaction events
func (er *EventRouter) SubscribeToAllEvents() chan BlockchainEvent {
	return er.eventBus.SubscribeToAllEvents()
}

// Unsubscribe removes a subscription for a transaction hash
func (er *EventRouter) Unsubscribe(txHash string, ch chan BlockchainEvent) {
	er.eventBus.Unsubscribe(txHash, ch)
}

// UnsubscribeFromAllEvents removes an all-events subscription
func (er *EventRouter) UnsubscribeFromAllEvents(ch chan BlockchainEvent) {
	er.eventBus.UnsubscribeFromAllEvents(ch)
}
