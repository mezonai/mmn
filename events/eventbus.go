package events

import (
	"fmt"
	"sync"
)

// EventBus handles subscription and publishing of blockchain events
type EventBus struct {
	subscribers []chan BlockchainEvent
	mu          sync.RWMutex
}

// NewEventBus creates a new EventBus instance
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make([]chan BlockchainEvent, 0),
	}
}

// Subscribe subscribes to all transaction events
func (eb *EventBus) Subscribe() chan BlockchainEvent {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	ch := make(chan BlockchainEvent, 50) // Buffer for events
	eb.subscribers = append(eb.subscribers, ch)
	
	fmt.Printf("[EventBus] Client subscribed to transaction events (total subscribers: %d)\n", len(eb.subscribers))
	return ch
}

// Unsubscribe removes a subscription
func (eb *EventBus) Unsubscribe(ch chan BlockchainEvent) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	for i, subscriber := range eb.subscribers {
		if subscriber == ch {
			// Remove the subscription
			eb.subscribers = append(eb.subscribers[:i], eb.subscribers[i+1:]...)
			close(ch)
			
			fmt.Printf("[EventBus] Client unsubscribed from events (remaining subscribers: %d)\n", len(eb.subscribers))
			break
		}
	}
}

// Publish publishes an event to all subscribers
func (eb *EventBus) Publish(event BlockchainEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	txHash := event.TxHash()

	// Notify subscribers
	if len(eb.subscribers) > 0 {
		fmt.Printf("[EventBus] Publishing %s event for tx: %s to %d subscribers\n", event.Type(), txHash, len(eb.subscribers))
		
		for _, ch := range eb.subscribers {
			select {
			case ch <- event:
				// Event sent successfully
			default:
				// Channel is full, skip this subscriber
				fmt.Printf("[EventBus] Warning: subscriber channel full for tx: %s\n", txHash)
			}
		}
	} else {
		fmt.Printf("[EventBus] No subscribers for event: %s (tx: %s)\n", event.Type(), txHash)
	}
}

// GetTotalSubscriptions returns the total number of active subscriptions
func (eb *EventBus) GetTotalSubscriptions() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	
	return len(eb.subscribers)
}
