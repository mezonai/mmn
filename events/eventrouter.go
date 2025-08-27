package events

// EventRouter handles complex event routing logic
type EventRouter struct {
	eventBus *EventBus
}

// NewEventRouter creates a new EventRouter instance
func NewEventRouter(eventBus *EventBus) *EventRouter {
	return &EventRouter{
		eventBus: eventBus,
	}
}

// PublishTransactionEvent publishes a transaction-specific event
func (er *EventRouter) PublishTransactionEvent(event BlockchainEvent) {
	er.eventBus.Publish(event)
}

// Subscribe subscribes to all transaction events
func (er *EventRouter) Subscribe() (SubscriberID, chan BlockchainEvent) {
	return er.eventBus.Subscribe()
}

// Unsubscribe removes a subscription by ID
func (er *EventRouter) Unsubscribe(id SubscriberID) bool {
	return er.eventBus.Unsubscribe(id)
}
