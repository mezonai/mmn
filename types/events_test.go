package types

import (
	"fmt"
	"testing"
	"time"
)

// mockBlockStore implements BlockStore interface for testing
type mockBlockStore struct {
	txHashes map[uint64][]string
}

func (m *mockBlockStore) GetTransactionHashes(slot uint64) []string {
	return m.txHashes[slot]
}

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

func TestEventRouter(t *testing.T) {
	// Mock BlockStore for testing
	mockBlockStore := &mockBlockStore{
		txHashes: map[uint64][]string{
			123: {"tx1", "tx2", "tx3"},
		},
	}
	
	eventBus := NewEventBus()
	eventRouter := NewEventRouter(eventBus, mockBlockStore)
	
	// Subscribe to a transaction that will be in the block
	txHash := "tx1"
	eventChan := eventRouter.Subscribe(txHash)
	
	// Publish block finalization event
	go func() {
		eventRouter.PublishBlockFinalized(123, "block-hash")
	}()
	
	// Wait for event
	select {
	case receivedEvent := <-eventChan:
		if receivedEvent.Type() != "BlockFinalized" {
			t.Errorf("Expected BlockFinalized, got %s", receivedEvent.Type())
		}
		
		// Verify it's the correct event type
		if blockEvent, ok := receivedEvent.(*BlockFinalized); ok {
			if blockEvent.BlockSlot() != 123 {
				t.Errorf("Expected block slot 123, got %d", blockEvent.BlockSlot())
			}
			if blockEvent.BlockHash() != "block-hash" {
				t.Errorf("Expected block hash 'block-hash', got %s", blockEvent.BlockHash())
			}
		} else {
			t.Error("Received event is not a BlockFinalized event")
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for event")
	}
	
	// Test unsubscribe
	eventRouter.Unsubscribe(txHash, eventChan)
}

func TestEventRouterOnlyNotifiesRelevantTransactions(t *testing.T) {
	// Mock BlockStore for testing
	mockBlockStore := &mockBlockStore{
		txHashes: map[uint64][]string{
			123: {"tx1", "tx2", "tx3"}, // Only these transactions in block 123
		},
	}
	
	eventBus := NewEventBus()
	eventRouter := NewEventRouter(eventBus, mockBlockStore)
	
	// Subscribe to a transaction that will be in the block
	txInBlock := "tx1"
	eventChanInBlock := eventRouter.Subscribe(txInBlock)
	
	// Subscribe to a transaction that will NOT be in the block
	txNotInBlock := "tx-not-in-block"
	eventChanNotInBlock := eventRouter.Subscribe(txNotInBlock)
	
	// Publish block finalization event
	go func() {
		eventRouter.PublishBlockFinalized(123, "block-hash")
	}()
	
	// Wait for event on the transaction that should be notified
	select {
	case receivedEvent := <-eventChanInBlock:
		if receivedEvent.Type() != "BlockFinalized" {
			t.Errorf("Expected BlockFinalized, got %s", receivedEvent.Type())
		}
		fmt.Printf("✅ Transaction %s correctly received BlockFinalized event\n", txInBlock)
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for event on transaction in block")
	}
	
	// Verify that the transaction NOT in the block does NOT receive the event
	select {
	case <-eventChanNotInBlock:
		t.Error("Transaction not in block incorrectly received BlockFinalized event")
	case <-time.After(100 * time.Millisecond):
		fmt.Printf("✅ Transaction %s correctly did NOT receive BlockFinalized event\n", txNotInBlock)
	}
	
	// Cleanup
	eventRouter.Unsubscribe(txInBlock, eventChanInBlock)
	eventRouter.Unsubscribe(txNotInBlock, eventChanNotInBlock)
}
