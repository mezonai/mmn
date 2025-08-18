package events

import (
	"sync"

	"github.com/mezonai/mmn/logx"
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
	
	logx.Info("EVENTBUS", "Client subscribed to transaction events", "total_subscribers", len(eb.subscribers))
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
			
			logx.Info("EVENTBUS", "Client unsubscribed from events", "remaining_subscribers", len(eb.subscribers))
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
		logx.Info("EVENTBUS", "Publishing event", "event_type", event.Type(), "tx_hash", txHash, "subscribers", len(eb.subscribers))
		
		for _, ch := range eb.subscribers {
			select {
			case ch <- event:
				// Event sent successfully
			default:
				// Channel is full, skip this subscriber
				logx.Warn("EVENTBUS", "Subscriber channel full", "tx_hash", txHash)
			}
		}
	} else {
		logx.Info("EVENTBUS", "No subscribers for event", "event_type", event.Type(), "tx_hash", txHash)
	}
}

// GetTotalSubscriptions returns the total number of active subscriptions
func (eb *EventBus) GetTotalSubscriptions() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	
	return len(eb.subscribers)
}
