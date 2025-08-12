package events

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
	
	fmt.Printf("[EventBus] Client subscribed to specific transaction: %s (total subscribers: %d)\n", txHash, len(eb.subscribers[txHash]))
	return ch
}

// SubscribeToAllEvents subscribes to all transaction events
func (eb *EventBus) SubscribeToAllEvents() chan BlockchainEvent {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	ch := make(chan BlockchainEvent, 50) // Larger buffer for all events
	eb.subscribers["*"] = append(eb.subscribers["*"], ch)
	
	fmt.Printf("[EventBus] Client subscribed to all transaction events (total all-events subscribers: %d)\n", len(eb.subscribers["*"]))
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
				
				fmt.Printf("[EventBus] Client unsubscribed from specific transaction: %s (remaining subscribers: %d)\n", txHash, len(eb.subscribers[txHash]))
				break
			}
		}
	}
}

// UnsubscribeFromAllEvents removes an all-events subscription
func (eb *EventBus) UnsubscribeFromAllEvents(ch chan BlockchainEvent) {
	eb.Unsubscribe("*", ch)
}

// Publish publishes an event to all relevant subscribers
func (eb *EventBus) Publish(event BlockchainEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	txHash := event.TxHash()

	// For transaction-specific events, notify subscribers for that transaction
	if subs, exists := eb.subscribers[txHash]; exists {
		fmt.Printf("[EventBus] Publishing %s event for tx: %s to %d specific subscribers\n", event.Type(), txHash, len(subs))
		
		for _, ch := range subs {
			select {
			case ch <- event:
				// Event sent successfully
			default:
				// Channel is full, skip this subscriber
				fmt.Printf("[EventBus] Warning: specific subscriber channel full for tx: %s\n", txHash)
			}
		}
	} else {
		fmt.Printf("[EventBus] No specific subscribers for tx: %s (event: %s)\n", txHash, event.Type())
	}

	// Also notify all-events subscribers
	if allSubs, exists := eb.subscribers["*"]; exists {
		fmt.Printf("[EventBus] Publishing %s event for tx: %s to %d all-events subscribers\n", event.Type(), txHash, len(allSubs))
		
		for _, ch := range allSubs {
			select {
			case ch <- event:
				// Event sent successfully
			default:
				// Channel is full, skip this subscriber
				fmt.Printf("[EventBus] Warning: all-events subscriber channel full for tx: %s\n", txHash)
			}
		}
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

// GetTrackedTransactions returns a list of all transaction hashes being tracked
func (eb *EventBus) GetTrackedTransactions() []string {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	
	txHashes := make([]string, 0, len(eb.subscribers))
	for txHash := range eb.subscribers {
		txHashes = append(txHashes, txHash)
	}
	return txHashes
}
