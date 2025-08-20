package events

import (
	"fmt"
	"testing"
	"time"

	"github.com/mezonai/mmn/types"
)

func TestEventBus(t *testing.T) {
	eventBus := NewEventBus()

	// Test subscription to all events
	subscriberID, eventChan := eventBus.Subscribe()

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
		if receivedEvent.Type() != EventTransactionAddedToMempool {
			t.Errorf("Expected %s, got %s", EventTransactionAddedToMempool, receivedEvent.Type())
		}
		if receivedEvent.TxHash() != txHash {
			t.Errorf("Expected txHash %s, got %s", txHash, receivedEvent.TxHash())
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for event")
	}

	// Test unsubscribe
	if !eventBus.Unsubscribe(subscriberID) {
		t.Error("Failed to unsubscribe")
	}

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
	if event.Type() != EventTransactionAddedToMempool {
		t.Errorf("Expected %s, got %s", EventTransactionAddedToMempool, event.Type())
	}

	// Test TransactionIncludedInBlock
	blockEvent := NewTransactionIncludedInBlock("tx-hash", 123, "block-hash")
	if blockEvent.Type() != EventTransactionIncludedInBlock {
		t.Errorf("Expected %s, got %s", EventTransactionIncludedInBlock, blockEvent.Type())
	}
	if blockEvent.BlockSlot() != 123 {
		t.Errorf("Expected block slot 123, got %d", blockEvent.BlockSlot())
	}

	// Test TransactionFailed
	failedEvent := NewTransactionFailed("tx-hash", "insufficient funds")
	if failedEvent.Type() != EventTransactionFailed {
		t.Errorf("Expected %s, got %s", EventTransactionFailed, failedEvent.Type())
	}
	if failedEvent.ErrorMessage() != "insufficient funds" {
		t.Errorf("Expected error message 'insufficient funds', got %s", failedEvent.ErrorMessage())
	}

	// Test TransactionFinalized
	finalizedEvent := NewTransactionFinalized("tx-hash", 123, "block-hash")
	if finalizedEvent.Type() != EventTransactionFinalized {
		t.Errorf("Expected %s, got %s", EventTransactionFinalized, finalizedEvent.Type())
	}
	if finalizedEvent.BlockSlot() != 123 {
		t.Errorf("Expected block slot 123, got %d", finalizedEvent.BlockSlot())
	}
	if finalizedEvent.BlockHash() != "block-hash" {
		t.Errorf("Expected block hash 'block-hash', got %s", finalizedEvent.BlockHash())
	}
}

func TestMultipleSubscribers(t *testing.T) {
	eventBus := NewEventBus()

	// Subscribe multiple clients to all events
	subscriberID1, eventChan1 := eventBus.Subscribe()
	subscriberID2, eventChan2 := eventBus.Subscribe()

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
	if !eventBus.Unsubscribe(subscriberID1) {
		t.Error("Failed to unsubscribe subscriber 1")
	}
	if !eventBus.Unsubscribe(subscriberID2) {
		t.Error("Failed to unsubscribe subscriber 2")
	}

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
	subscriberID, eventChan := eventRouter.Subscribe()
	defer eventRouter.Unsubscribe(subscriberID)
	
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

func TestTransactionAddedToMempool(t *testing.T) {
	tx := &types.Transaction{
		Type:      types.TxTypeTransfer,
		Sender:    "sender-address",
		Recipient: "recipient-address",
		Amount:    1000,
		Timestamp: uint64(time.Now().Unix()),
	}
	
	txHash := "test-tx-hash"
	event := NewTransactionAddedToMempool(txHash, tx)
	
	if event == nil {
		t.Fatal("NewTransactionAddedToMempool returned nil")
	}
	
	if event.Type() != EventTransactionAddedToMempool {
		t.Errorf("Expected event type %s, got %s", EventTransactionAddedToMempool, event.Type())
	}
	
	if event.TxHash() != txHash {
		t.Errorf("Expected tx hash %s, got %s", txHash, event.TxHash())
	}
	
	if event.Transaction() != tx {
		t.Error("Transaction reference does not match")
	}
	
	// Check timestamp is recent
	now := time.Now()
	if event.Timestamp().Sub(now) > time.Second {
		t.Errorf("Event timestamp %v is too far from current time %v", event.Timestamp(), now)
	}
}

func TestTransactionIncludedInBlock(t *testing.T) {
	txHash := "test-tx-hash"
	blockSlot := uint64(12345)
	blockHash := "block-hash-123"
	
	event := NewTransactionIncludedInBlock(txHash, blockSlot, blockHash)
	
	if event == nil {
		t.Fatal("NewTransactionIncludedInBlock returned nil")
	}
	
	if event.Type() != EventTransactionIncludedInBlock {
		t.Errorf("Expected event type %s, got %s", EventTransactionIncludedInBlock, event.Type())
	}
	
	if event.TxHash() != txHash {
		t.Errorf("Expected tx hash %s, got %s", txHash, event.TxHash())
	}
	
	if event.BlockSlot() != blockSlot {
		t.Errorf("Expected block slot %d, got %d", blockSlot, event.BlockSlot())
	}
	
	if event.BlockHash() != blockHash {
		t.Errorf("Expected block hash %s, got %s", blockHash, event.BlockHash())
	}
	
	// Check timestamp is recent
	now := time.Now()
	if event.Timestamp().Sub(now) > time.Second {
		t.Errorf("Event timestamp %v is too far from current time %v", event.Timestamp(), now)
	}
}

func TestTransactionFinalized(t *testing.T) {
	txHash := "test-tx-hash"
	blockSlot := uint64(12345)
	blockHash := "block-hash-123"
	
	event := NewTransactionFinalized(txHash, blockSlot, blockHash)
	
	if event == nil {
		t.Fatal("NewTransactionFinalized returned nil")
	}
	
	if event.Type() != EventTransactionFinalized {
		t.Errorf("Expected event type %s, got %s", EventTransactionFinalized, event.Type())
	}
	
	if event.TxHash() != txHash {
		t.Errorf("Expected tx hash %s, got %s", txHash, event.TxHash())
	}
	
	if event.BlockSlot() != blockSlot {
		t.Errorf("Expected block slot %d, got %d", blockSlot, event.BlockSlot())
	}
	
	if event.BlockHash() != blockHash {
		t.Errorf("Expected block hash %s, got %s", blockHash, event.BlockHash())
	}
	
	// Check timestamp is recent
	now := time.Now()
	if event.Timestamp().Sub(now) > time.Second {
		t.Errorf("Event timestamp %v is too far from current time %v", event.Timestamp(), now)
	}
}

func TestEventBusConcurrentPublishing(t *testing.T) {
	eventBus := NewEventBus()
	subscriberID, eventChan := eventBus.Subscribe()
	defer eventBus.Unsubscribe(subscriberID)
	
	// Publish multiple events concurrently
	numEvents := 10
	events := make([]BlockchainEvent, numEvents)
	
	for i := 0; i < numEvents; i++ {
		txHash := fmt.Sprintf("tx-hash-%d", i)
		events[i] = NewTransactionFailed(txHash, "test error")
	}
	
	// Publish all events concurrently
	for _, event := range events {
		go eventBus.Publish(event)
	}
	
	// Collect all events
	receivedEvents := make([]BlockchainEvent, 0, numEvents)
	timeout := time.After(2 * time.Second)
	
	for i := 0; i < numEvents; i++ {
		select {
		case event := <-eventChan:
			receivedEvents = append(receivedEvents, event)
		case <-timeout:
			t.Errorf("Timeout waiting for event %d", i)
			return
		}
	}
	
	if len(receivedEvents) != numEvents {
		t.Errorf("Expected %d events, got %d", numEvents, len(receivedEvents))
	}
}
