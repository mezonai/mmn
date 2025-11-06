package events

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/logx"
)

const (
	DEFAULT_BUFFER     = 256
	HEARTBEAT_INTERVAL = 30 * time.Second
)

type SubscriberID string

type Subscriber struct {
	ID      SubscriberID
	Channel chan BlockchainEvent
	closed  atomic.Bool
}

type EventBus struct {
	subscribers sync.Map      // map[SubscriberID]*Subscriber
	count       atomic.Uint64 // atomic counter for subscriber count
	closed      atomic.Bool   // indicates bus is shut down
	buffer      int           // default channel buffer size per subscriber
}

func NewEventBus() *EventBus {
	eb := &EventBus{buffer: DEFAULT_BUFFER}
	exception.SafeGoWithPanic("EventBusHeartbeat", func() {
		eb.heartbeat()
	})
	return eb
}

func (eb *EventBus) generateUUIDID() SubscriberID {
	id := uuid.Must(uuid.NewV7())
	return SubscriberID(id.String())
}

func (eb *EventBus) Subscribe() (SubscriberID, chan BlockchainEvent) {
	if eb.closed.Load() {
		logx.Warn("EVENTBUS", "Subscribe called on closed EventBus")
		ch := make(chan BlockchainEvent)
		close(ch)
		return "", ch
	}
	id := eb.generateUUIDID()

	ch := make(chan BlockchainEvent, eb.buffer) // Buffer for events
	subscriber := &Subscriber{
		ID:      id,
		Channel: ch,
	}

	eb.subscribers.Store(id, subscriber)
	eb.count.Add(1)

	logx.Info("EVENTBUS", fmt.Sprintf("Client subscribed to transaction events | subscriber_id=%s | total_subscribers=%d", id, eb.GetTotalSubscriptions()))

	return id, ch
}

// SetDefaultBuffer sets the default channel buffer size for new subscriptions
func (eb *EventBus) SetBuffer(buffer int) {
	if buffer <= 0 {
		buffer = DEFAULT_BUFFER
	}
	eb.buffer = buffer
}

// Unsubscribe removes a subscription by ID
func (eb *EventBus) Unsubscribe(id SubscriberID) bool {
	value, exists := eb.subscribers.Load(id)
	if !exists {
		logx.Warn("EVENTBUS", fmt.Sprintf("Attempted to unsubscribe non-existent subscriber | subscriber_id=%s", id))
		return false
	}
	subscriber := value.(*Subscriber)
	subscriber.closed.Store(true)
	eb.subscribers.Delete(id)
	eb.count.Add(^uint64(0)) // equivalent to subtract 1

	logx.Info("EVENTBUS", fmt.Sprintf("Client unsubscribed from events | subscriber_id=%s | remaining_subscribers=%d", id, eb.GetTotalSubscriptions()))
	return true
}

func (eb *EventBus) heartbeat() {
	heartBeatEvent := &HeartBeatEvent{
		timestamp: time.Now(),
	}

	ticker := time.NewTicker(HEARTBEAT_INTERVAL)
	defer ticker.Stop()

	for range ticker.C {
		logx.Info("EVENTBUS", fmt.Sprintf("Heartbeat | total_subscribers=%d", eb.GetTotalSubscriptions()))
		exception.SafeGoWithPanic("EventBusHeartbeat", func() {
			eb.Publish(heartBeatEvent)
		})
	}
}

// Publish publishes an event to all subscribers
func (eb *EventBus) Publish(event BlockchainEvent) {
	if eb.closed.Load() {
		return
	}
	txHash := event.Transaction().Hash()
	totalSubscribers := eb.GetTotalSubscriptions()

	// Notify subscribers
	if totalSubscribers > 0 {
		logx.Info("EVENTBUS", fmt.Sprintf("Publishing event | event_type=%s | tx_hash=%s | subscribers=%d", event.Type(), txHash, totalSubscribers))

		eb.subscribers.Range(func(key, value interface{}) bool {
			id := key.(SubscriberID)
			subscriber := value.(*Subscriber)

			// Skip if unsubscribed concurrently
			if subscriber.closed.Load() {
				return true
			}

			select {
			case subscriber.Channel <- event:
				// Event sent successfully
			default:
				// Channel is full, skip this subscriber
				logx.Warn("EVENTBUS", fmt.Sprintf("Subscriber channel full | subscriber_id=%s | tx_hash=%s", id, txHash))
			}
			return true // continue iteration
		})
	} else {
		logx.Info("EVENTBUS", fmt.Sprintf("No subscribers for event | event_type=%s | tx_hash=%s", event.Type(), txHash))
	}
}

// GetTotalSubscriptions returns the total number of active subscriptions
func (eb *EventBus) GetTotalSubscriptions() int {
	return int(eb.count.Load())
}

// GetSubscriberIDs returns a slice of all active subscriber IDs
func (eb *EventBus) GetSubscriberIDs() []SubscriberID {
	var ids []SubscriberID
	eb.subscribers.Range(func(key, value interface{}) bool {
		id := key.(SubscriberID)
		ids = append(ids, id)
		return true
	})
	return ids
}

// HasSubscriber checks if a subscriber with the given ID exists
func (eb *EventBus) HasSubscriber(id SubscriberID) bool {
	_, exists := eb.subscribers.Load(id)
	return exists
}

// Shutdown stops the EventBus: prevents new subscriptions and publications,
// and clears all current subscribers without closing their channels.
func (eb *EventBus) Shutdown() {
	if eb.closed.Swap(true) {
		return
	}
	eb.subscribers.Range(func(key, _ interface{}) bool {
		eb.subscribers.Delete(key)
		return true
	})
	eb.count.Store(0)
	logx.Info("EVENTBUS", "EventBus shutdown completed")
}
