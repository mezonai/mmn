package events

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/mezonai/mmn/logx"
)

// SubscriberID represents a unique identifier for a subscriber
type SubscriberID uint64

// Subscriber represents a subscriber with its channel and metadata
type Subscriber struct {
	ID      SubscriberID
	Channel chan BlockchainEvent
}

// EventBus handles subscription and publishing of blockchain events
type EventBus struct {
	subscribers map[SubscriberID]*Subscriber
	mu          sync.RWMutex
	nextID      uint64 // Atomic counter for generating unique IDs
}

// NewEventBus creates a new EventBus instance
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[SubscriberID]*Subscriber),
		nextID:      0,
	}
}

// Subscribe subscribes to all transaction events and returns a subscriber ID
func (eb *EventBus) Subscribe() (SubscriberID, chan BlockchainEvent) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	// Generate unique subscriber ID
	id := SubscriberID(atomic.AddUint64(&eb.nextID, 1))

	ch := make(chan BlockchainEvent, 50) // Buffer for events
	subscriber := &Subscriber{
		ID:      id,
		Channel: ch,
	}

	eb.subscribers[id] = subscriber

	logx.Info("EVENTBUS", fmt.Sprintf("Client subscribed to transaction events | subscriber_id=%d | total_subscribers=%d", id, len(eb.subscribers)))

	return id, ch
}

// Unsubscribe removes a subscription by ID
func (eb *EventBus) Unsubscribe(id SubscriberID) bool {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	subscriber, exists := eb.subscribers[id]
	if !exists {
		logx.Warn("EVENTBUS", fmt.Sprintf("Attempted to unsubscribe non-existent subscriber | subscriber_id=%d", id))
		return false
	}

	// Remove the subscription
	delete(eb.subscribers, id)
	close(subscriber.Channel)

	logx.Info("EVENTBUS", fmt.Sprintf("Client unsubscribed from events | subscriber_id=%d | remaining_subscribers=%d", id, len(eb.subscribers)))
	return true
}

// Publish publishes an event to all subscribers
func (eb *EventBus) Publish(event BlockchainEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	txHash := event.TxHash()

	// Notify subscribers
	if len(eb.subscribers) > 0 {
		logx.Info("EVENTBUS", fmt.Sprintf("Publishing event | event_type=%s | tx_hash=%s | subscribers=%d", event.Type(), txHash, len(eb.subscribers)))

		for id, subscriber := range eb.subscribers {
			select {
			case subscriber.Channel <- event:
				// Event sent successfully
			default:
				// Channel is full, skip this subscriber
				logx.Warn("EVENTBUS", fmt.Sprintf("Subscriber channel full | subscriber_id=%d | tx_hash=%s", id, txHash))
			}
		}
	} else {
		logx.Info("EVENTBUS", fmt.Sprintf("No subscribers for event | event_type=%s | tx_hash=%s", event.Type(), txHash))
	}
}

// GetTotalSubscriptions returns the total number of active subscriptions
func (eb *EventBus) GetTotalSubscriptions() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	return len(eb.subscribers)
}

// GetSubscriberIDs returns a slice of all active subscriber IDs
func (eb *EventBus) GetSubscriberIDs() []SubscriberID {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	ids := make([]SubscriberID, 0, len(eb.subscribers))
	for id := range eb.subscribers {
		ids = append(ids, id)
	}
	return ids
}

// HasSubscriber checks if a subscriber with the given ID exists
func (eb *EventBus) HasSubscriber(id SubscriberID) bool {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	_, exists := eb.subscribers[id]
	return exists
}
