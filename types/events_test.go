package types

import (
	"testing"
	"time"
)

func TestEventBus(t *testing.T) {
	eventBus := NewEventBus()
	
	// Test subscription
	txHash := "test-tx-hash"
	eventChan := eventBus.Subscribe(txHash)
	
	// Verify subscription count
	if count := eventBus.GetSubscriberCount(txHash); count != 1 {
		t.Errorf("Expected 1 subscriber, got %d", count)
	}
	
	// Test publishing event
	tx := &Transaction{
		Type:      TxTypeTransfer,
		Sender:    "sender",
		Recipient: "recipient",
		Amount:    100,
		Timestamp: uint64(time.Now().Unix()),
	}
	
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
	eventBus.Unsubscribe(txHash, eventChan)
	
	// Verify subscription count is 0
	if count := eventBus.GetSubscriberCount(txHash); count != 0 {
		t.Errorf("Expected 0 subscribers after unsubscribe, got %d", count)
	}
}

func TestBlockchainEvents(t *testing.T) {
	// Test TransactionAddedToMempool
	tx := &Transaction{
		Type:      TxTypeTransfer,
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
	
	// Test BlockFinalized
	finalizedEvent := NewBlockFinalized(123, "block-hash")
	if finalizedEvent.Type() != "BlockFinalized" {
		t.Errorf("Expected BlockFinalized, got %s", finalizedEvent.Type())
	}
	if finalizedEvent.BlockSlot() != 123 {
		t.Errorf("Expected block slot 123, got %d", finalizedEvent.BlockSlot())
	}
}
