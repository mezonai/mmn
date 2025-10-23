package events

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/utils"
)

type SubscriberID string

type Subscriber struct {
	ID      SubscriberID
	Channel chan BlockchainEvent
}

type EventBus struct {
	subscribers sync.Map      // map[SubscriberID]*Subscriber
	count       atomic.Uint64 // atomic counter for subscriber count
}

func NewEventBus() *EventBus {
	return &EventBus{}
}

func (eb *EventBus) generateUUIDID() SubscriberID {
	id := uuid.Must(uuid.NewV7())
	return SubscriberID(id.String())
}

func (eb *EventBus) Subscribe() (SubscriberID, chan BlockchainEvent) {
	id := eb.generateUUIDID()

	ch := make(chan BlockchainEvent, 50) // Buffer for events
	subscriber := &Subscriber{
		ID:      id,
		Channel: ch,
	}

	eb.subscribers.Store(id, subscriber)
	eb.count.Add(1)

	logx.Info("EVENTBUS", fmt.Sprintf("Client subscribed to transaction events | subscriber_id=%s | total_subscribers=%d", id, eb.GetTotalSubscriptions()))

	return id, ch
}

// Unsubscribe removes a subscription by ID
func (eb *EventBus) Unsubscribe(id SubscriberID) bool {
	value, exists := eb.subscribers.LoadAndDelete(id)
	if !exists {
		logx.Warn("EVENTBUS", fmt.Sprintf("Attempted to unsubscribe non-existent subscriber | subscriber_id=%s", utils.ShortenLog(string(id))))
		return false
	}

	subscriber := value.(*Subscriber)
	close(subscriber.Channel)
	eb.count.Add(^uint64(0)) // equivalent to subtract 1

	logx.Info("EVENTBUS", fmt.Sprintf("Client unsubscribed from events | subscriber_id=%s | remaining_subscribers=%d", id, eb.GetTotalSubscriptions()))
	return true
}

// Publish publishes an event to all subscribers
func (eb *EventBus) Publish(event BlockchainEvent) {
	txHash := event.TxHash()
	totalSubscribers := eb.GetTotalSubscriptions()

	// Notify subscribers
	if totalSubscribers > 0 {
		logx.Info("EVENTBUS", fmt.Sprintf("Publishing event | event_type=%s | tx_hash=%s | subscribers=%d", event.Type(), txHash, totalSubscribers))

		eb.subscribers.Range(func(key, value interface{}) bool {
			id := key.(SubscriberID)
			subscriber := value.(*Subscriber)

			select {
			case subscriber.Channel <- event:
				// Event sent successfully
			default:
				// Channel is full, skip this subscriber
				logx.Warn("EVENTBUS", fmt.Sprintf("Subscriber channel full | subscriber_id=%s | tx_hash=%s", utils.ShortenLog(string(id)), utils.ShortenLog(string(txHash))))
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
