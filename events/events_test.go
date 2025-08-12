package events

import (
	"mmn/types"
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
	tx := &types.Transaction{
		Type:      types.TxTypeTransfer,
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
	
	// Test BlockFinalized
	blockFinalizedEvent := NewBlockFinalized(123, "block-hash")
	if blockFinalizedEvent.Type() != "BlockFinalized" {
		t.Errorf("Expected BlockFinalized, got %s", blockFinalizedEvent.Type())
	}
	if blockFinalizedEvent.BlockSlot() != 123 {
		t.Errorf("Expected block slot 123, got %d", blockFinalizedEvent.BlockSlot())
	}
	if blockFinalizedEvent.BlockHash() != "block-hash" {
		t.Errorf("Expected block hash 'block-hash', got %s", blockFinalizedEvent.BlockHash())
	}
}

func TestEventRouter(t *testing.T) {
	// Create mock block store
	mockBlockStore := &mockBlockStore{
		txHashes: map[uint64][]string{
			123: {"tx1", "tx2", "tx3"},
		},
	}
	
	eventBus := NewEventBus()
	eventRouter := NewEventRouter(eventBus, mockBlockStore)
	
	// Subscribe to a transaction that's in the block
	txHash := "tx1"
	eventChan := eventRouter.Subscribe(txHash)
	
	// Publish block finalized event
	eventRouter.PublishBlockFinalized(123, "block-hash")
	
	// Wait for event
	select {
	case event := <-eventChan:
		if event.Type() != "BlockFinalized" {
			t.Errorf("Expected BlockFinalized, got %s", event.Type())
		}
		if blockEvent, ok := event.(*BlockFinalized); ok {
			if blockEvent.BlockSlot() != 123 {
				t.Errorf("Expected block slot 123, got %d", blockEvent.BlockSlot())
			}
		} else {
			t.Error("Expected BlockFinalized event type")
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for event")
	}
	
	// Clean up
	eventRouter.Unsubscribe(txHash, eventChan)
}

func TestEventRouterOnlyNotifiesRelevantTransactions(t *testing.T) {
	// Create mock block store with specific transactions
	mockBlockStore := &mockBlockStore{
		txHashes: map[uint64][]string{
			123: {"tx-in-block"},
		},
	}
	
	eventBus := NewEventBus()
	eventRouter := NewEventRouter(eventBus, mockBlockStore)
	
	// Subscribe to a transaction that's in the block
	txInBlock := "tx-in-block"
	eventChanInBlock := eventRouter.Subscribe(txInBlock)
	
	// Subscribe to a transaction that's NOT in the block
	txNotInBlock := "tx-not-in-block"
	eventChanNotInBlock := eventRouter.Subscribe(txNotInBlock)
	
	// Publish block finalized event
	eventRouter.PublishBlockFinalized(123, "block-hash")
	
	// Only the transaction in the block should receive the event
	select {
	case event := <-eventChanInBlock:
		if event.Type() != "BlockFinalized" {
			t.Errorf("Expected BlockFinalized, got %s", event.Type())
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for event for tx-in-block")
	}
	
	// The transaction not in the block should NOT receive the event
	select {
	case <-eventChanNotInBlock:
		t.Error("Transaction not in block should not have received event")
	case <-time.After(100 * time.Millisecond):
		// This is expected - no event should be received
	}
	
	// Clean up
	eventRouter.Unsubscribe(txInBlock, eventChanInBlock)
	eventRouter.Unsubscribe(txNotInBlock, eventChanNotInBlock)
}
