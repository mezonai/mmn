package types

import (
	"fmt"
	"sync"
)

// EventBus handles subscription and publishing of blockchain events
type EventBus struct {
	subscribers map[string][]chan BlockchainEvent
	mu          sync.RWMutex
}

// NewEventBus creates a new EventBus instance
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string][]chan BlockchainEvent),
	}
}

// Subscribe subscribes to events for a specific transaction hash
func (eb *EventBus) Subscribe(txHash string) chan BlockchainEvent {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	ch := make(chan BlockchainEvent, 10) // Buffered channel to prevent blocking
	eb.subscribers[txHash] = append(eb.subscribers[txHash], ch)
	
	fmt.Printf("Subscribed to events for tx: %s (total subscribers: %d)\n", txHash, len(eb.subscribers[txHash]))
	return ch
}

// Unsubscribe removes a subscription for a transaction hash
func (eb *EventBus) Unsubscribe(txHash string, ch chan BlockchainEvent) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if subs, exists := eb.subscribers[txHash]; exists {
		for i, sub := range subs {
			if sub == ch {
				// Remove the subscription
				eb.subscribers[txHash] = append(subs[:i], subs[i+1:]...)
				close(ch)
				
				// Clean up empty subscriber lists
				if len(eb.subscribers[txHash]) == 0 {
					delete(eb.subscribers, txHash)
				}
				
				fmt.Printf("Unsubscribed from events for tx: %s (remaining subscribers: %d)\n", txHash, len(eb.subscribers[txHash]))
				break
			}
		}
	}
}

// Publish publishes an event to all relevant subscribers
func (eb *EventBus) Publish(event BlockchainEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	txHash := event.TxHash()

	// For transaction-specific events, notify subscribers for that transaction
	if subs, exists := eb.subscribers[txHash]; exists {
		fmt.Printf("Publishing %s event for tx: %s to %d subscribers\n", event.Type(), txHash, len(subs))
		
		for _, ch := range subs {
			select {
			case ch <- event:
				// Event sent successfully
			default:
				// Channel is full, skip this subscriber
				fmt.Printf("Warning: subscriber channel full for tx: %s\n", txHash)
			}
		}
	} else {
		fmt.Printf("No subscribers for tx: %s (event: %s)\n", txHash, event.Type())
	}
}

// GetSubscriberCount returns the number of subscribers for a transaction
func (eb *EventBus) GetSubscriberCount(txHash string) int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	
	if subs, exists := eb.subscribers[txHash]; exists {
		return len(subs)
	}
	return 0
}

// GetTotalSubscriptions returns the total number of active subscriptions
func (eb *EventBus) GetTotalSubscriptions() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	
	total := 0
	for _, subs := range eb.subscribers {
		total += len(subs)
	}
	return total
}

// GetTrackedTransactions returns all transaction hashes being tracked
func (eb *EventBus) GetTrackedTransactions() []string {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	
	txs := make([]string, 0, len(eb.subscribers))
	for txHash := range eb.subscribers {
		txs = append(txs, txHash)
	}
	return txs
}
