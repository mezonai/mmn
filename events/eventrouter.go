package events

import (
	"fmt"
	"sync"
	"time"
)

// EventRouter handles complex event routing logic
type EventRouter struct {
	eventBus *EventBus
	mu       sync.RWMutex
}

// NewEventRouter creates a new EventRouter instance
func NewEventRouter(eventBus *EventBus) *EventRouter {
	return &EventRouter{
		eventBus: eventBus,
	}
}

// PublishBlockFinalized publishes a block finalization event to relevant transaction subscribers
func (er *EventRouter) PublishBlockFinalized(slot uint64, blockHash string) {
	er.mu.RLock()
	defer er.mu.RUnlock()

	// Create minimal block event (no transaction hashes)
	event := NewBlockFinalized(slot, blockHash)

	fmt.Printf("[EventRouter] Publishing BlockFinalized for slot %d\n", slot)

	// Use EventBus.Publish to handle subscribers
	er.eventBus.Publish(event)
}

// PublishBlockFinalizedWithTimestamp publishes a block finalization event with a specific timestamp
func (er *EventRouter) PublishBlockFinalizedWithTimestamp(slot uint64, blockHash string, timestamp time.Time) {
	er.mu.RLock()
	defer er.mu.RUnlock()

	// Create block event with specific timestamp
	event := NewBlockFinalizedWithTimestamp(slot, blockHash, timestamp)

	fmt.Printf("[EventRouter] Publishing BlockFinalized for slot %d \n", slot)

	// Use EventBus.Publish to handle subscribers
	er.eventBus.Publish(event)
}

// PublishTransactionEvent publishes a transaction-specific event
func (er *EventRouter) PublishTransactionEvent(event BlockchainEvent) {
	er.eventBus.Publish(event)
}

// Subscribe subscribes to all transaction events
func (er *EventRouter) Subscribe() chan BlockchainEvent {
	return er.eventBus.Subscribe()
}

// Unsubscribe removes a subscription
func (er *EventRouter) Unsubscribe(ch chan BlockchainEvent) {
	er.eventBus.Unsubscribe(ch)
}
