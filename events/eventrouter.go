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
	
	// Route to relevant subscribers only
	notifiedCount := 0
	for _, txHash := range txHashes {
		if subs, exists := er.eventBus.subscribers[txHash]; exists {
			fmt.Printf("EventRouter: Notifying %d subscribers for tx: %s (in block %d)\n", len(subs), txHash, slot)
			for _, ch := range subs {
				select {
				case ch <- event:
					// Event sent successfully
					notifiedCount++
				default:
					// Channel is full, skip this subscriber
					fmt.Printf("Warning: subscriber channel full for tx: %s during block event\n", txHash)
				}
			}
		}
	}
	
	fmt.Printf("EventRouter: BlockFinalized for slot %d: notified %d subscribers for %d transactions\n", slot, notifiedCount, len(txHashes))
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
	
	// Route to relevant subscribers only
	notifiedCount := 0
	for _, txHash := range txHashes {
		if subs, exists := er.eventBus.subscribers[txHash]; exists {
			fmt.Printf("EventRouter: Notifying %d subscribers for tx: %s (in block %d)\n", len(subs), txHash, slot)
			for _, ch := range subs {
				select {
				case ch <- event:
					// Event sent successfully
					notifiedCount++
				default:
					// Channel is full, skip this subscriber
					fmt.Printf("Warning: subscriber channel full for tx: %s during block event\n", txHash)
				}
			}
		}
	}
	
	fmt.Printf("EventRouter: BlockFinalized for slot %d: notified %d subscribers for %d transactions\n", slot, notifiedCount, len(txHashes))
}

// PublishTransactionEvent publishes a transaction-specific event
func (er *EventRouter) PublishTransactionEvent(event BlockchainEvent) {
	er.eventBus.Publish(event)
}

// Subscribe subscribes to events for a specific transaction hash
func (er *EventRouter) Subscribe(txHash string) chan BlockchainEvent {
	return er.eventBus.Subscribe(txHash)
}

// Unsubscribe removes a subscription for a transaction hash
func (er *EventRouter) Unsubscribe(txHash string, ch chan BlockchainEvent) {
	er.eventBus.Unsubscribe(txHash, ch)
}
