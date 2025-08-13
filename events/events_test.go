package events

import (
	"mmn/types"
	"testing"
	"time"
)

func TestEventBus(t *testing.T) {
	eventBus := NewEventBus()

	// Test subscription to all events
	eventChan := eventBus.Subscribe()

	// Verify subscription count
	if count := eventBus.GetTotalSubscriptions(); count != 1 {
		t.Errorf("Expected 1 subscriber, got %d", count)
	}

	// Test publishing event
	tx := &types.Transaction{
		Type:      types.TxTypeTransfer,
		Sender:    "sender",
		Recipient: "recipient",
		Amount:    100,
		Timestamp: uint64(time.Now().Unix()),
	}

	txHash := "test-tx-hash"
	event := NewTransactionAddedToMempool(txHash, tx)

	// Publish event in goroutine to avoid blocking
	go func() {
		eventBus.Publish(event)
	}()

	// Wait for event
	select {
	case receivedEvent := <-eventChan:
		if receivedEvent.Type() != "TransactionAddedToMempool" {
			t.Errorf("Expected TransactionAddedToMempool, got %s", receivedEvent.Type())
		}
		if receivedEvent.TxHash() != txHash {
			t.Errorf("Expected txHash %s, got %s", txHash, receivedEvent.TxHash())
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for event")
	}

	// Test unsubscribe
	eventBus.Unsubscribe(eventChan)

	// Verify subscription count is 0
	if count := eventBus.GetTotalSubscriptions(); count != 0 {
		t.Errorf("Expected 0 subscribers after unsubscribe, got %d", count)
	}
}

func TestBlockchainEvents(t *testing.T) {
	// Test TransactionAddedToMempool
	tx := &types.Transaction{
		Type:      types.TxTypeTransfer,
		Sender:    "sender",
		Recipient: "recipient",
		Amount:    100,
		Timestamp: uint64(time.Now().Unix()),
	}

	event := NewTransactionAddedToMempool("tx-hash", tx)
	if event.Type() != "TransactionAddedToMempool" {
		t.Errorf("Expected TransactionAddedToMempool, got %s", event.Type())
	}

	// Test TransactionIncludedInBlock
	blockEvent := NewTransactionIncludedInBlock("tx-hash", 123, "block-hash")
	if blockEvent.Type() != "TransactionIncludedInBlock" {
		t.Errorf("Expected TransactionIncludedInBlock, got %s", blockEvent.Type())
	}
	if blockEvent.BlockSlot() != 123 {
		t.Errorf("Expected block slot 123, got %d", blockEvent.BlockSlot())
	}

	// Test TransactionFailed
	failedEvent := NewTransactionFailed("tx-hash", "insufficient funds")
	if failedEvent.Type() != "TransactionFailed" {
		t.Errorf("Expected TransactionFailed, got %s", failedEvent.Type())
	}
	if failedEvent.ErrorMessage() != "insufficient funds" {
		t.Errorf("Expected error message 'insufficient funds', got %s", failedEvent.ErrorMessage())
	}
}

func TestMultipleSubscribers(t *testing.T) {
	eventBus := NewEventBus()

	// Subscribe multiple clients to all events
	eventChan1 := eventBus.Subscribe()
	eventChan2 := eventBus.Subscribe()

	// Verify subscription count
	if count := eventBus.GetTotalSubscriptions(); count != 2 {
		t.Errorf("Expected 2 subscribers, got %d", count)
	}

	// Test publishing event
	tx := &types.Transaction{
		Type:      types.TxTypeTransfer,
		Sender:    "sender",
		Recipient: "recipient",
		Amount:    100,
		Timestamp: uint64(time.Now().Unix()),
	}

	txHash := "test-tx-hash"
	event := NewTransactionAddedToMempool(txHash, tx)

	// Publish event
	eventBus.Publish(event)

	// Both subscribers should receive the event
	select {
	case receivedEvent := <-eventChan1:
		if receivedEvent.TxHash() != txHash {
			t.Errorf("Expected txHash %s, got %s", txHash, receivedEvent.TxHash())
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for event on channel 1")
	}

	select {
	case receivedEvent := <-eventChan2:
		if receivedEvent.TxHash() != txHash {
			t.Errorf("Expected txHash %s, got %s", txHash, receivedEvent.TxHash())
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for event on channel 2")
	}

	// Clean up
	eventBus.Unsubscribe(eventChan1)
	eventBus.Unsubscribe(eventChan2)

	// Verify subscription count is 0
	if count := eventBus.GetTotalSubscriptions(); count != 0 {
		t.Errorf("Expected 0 subscribers after unsubscribe, got %d", count)
	}
}

func TestNewTransactionFailed(t *testing.T) {
	txHash := "test-tx-hash"
	errorMessage := "insufficient funds"
	
	failedEvent := NewTransactionFailed(txHash, errorMessage)
	
	if failedEvent == nil {
		t.Fatal("NewTransactionFailed returned nil")
	}
	
	if failedEvent.Type() != EventTransactionFailed {
		t.Errorf("Expected event type %s, got %s", EventTransactionFailed, failedEvent.Type())
	}
	
	if failedEvent.TxHash() != txHash {
		t.Errorf("Expected tx hash %s, got %s", txHash, failedEvent.TxHash())
	}
	
	if failedEvent.ErrorMessage() != errorMessage {
		t.Errorf("Expected error message %s, got %s", errorMessage, failedEvent.ErrorMessage())
	}
	
	// Check timestamp is recent
	now := time.Now()
	if failedEvent.Timestamp().Sub(now) > time.Second {
		t.Errorf("Event timestamp %v is too far from current time %v", failedEvent.Timestamp(), now)
	}
}

func TestEventRouterPublishTransactionFailed(t *testing.T) {
	eventBus := NewEventBus()
	eventRouter := NewEventRouter(eventBus)
	
	// Subscribe to events
	eventChan := eventRouter.Subscribe()
	defer eventRouter.Unsubscribe(eventChan)
	
	// Create and publish a failed transaction event
	txHash := "failed-tx-hash"
	errorMessage := "invalid signature"
	failedEvent := NewTransactionFailed(txHash, errorMessage)
	
	eventRouter.PublishTransactionEvent(failedEvent)
	
	// Wait for event
	select {
	case receivedEvent := <-eventChan:
		if receivedEvent.Type() != EventTransactionFailed {
			t.Errorf("Expected event type %s, got %s", EventTransactionFailed, receivedEvent.Type())
		}
		
		if receivedEvent.TxHash() != txHash {
			t.Errorf("Expected tx hash %s, got %s", txHash, receivedEvent.TxHash())
		}
		
		// Type assert to get error message
		if failedEvent, ok := receivedEvent.(*TransactionFailed); ok {
			if failedEvent.ErrorMessage() != errorMessage {
				t.Errorf("Expected error message %s, got %s", errorMessage, failedEvent.ErrorMessage())
			}
		} else {
			t.Error("Failed to type assert to TransactionFailed")
		}
		
	case <-time.After(time.Second):
		t.Error("Timeout waiting for event")
	}
}
