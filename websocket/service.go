package websocket

import (
	"log"
	"time"

	"mmn/events"
	"mmn/mempool"
)

// WSService manages WebSocket server and event integration
type WSService struct {
	server   *Server
	mempool  *mempool.Mempool
	eventBus *events.EventBus
}

// NewWSService creates a new WebSocket service
func NewWSService(mp *mempool.Mempool) *WSService {
	server := NewServer()
	eventBus := mp.GetEventBus()

	service := &WSService{
		server:   server,
		mempool:  mp,
		eventBus: eventBus,
	}

	// Subscribe to events
	service.subscribeToEvents()

	return service
}

// Start the WebSocket service
func (ws *WSService) Start(addr string) {
	log.Printf("Starting WebSocket service on %s", addr)

	// Start periodic mempool stats broadcasting
	go ws.startMempoolStatsReporter()

	// Start WebSocket server (blocking)
	ws.server.Start(addr)
}

// Subscribe to all relevant events
func (ws *WSService) subscribeToEvents() {
	// Subscribe to transaction submitted events
	ws.eventBus.Subscribe(events.TxSubmittedEvent, func(eventType events.EventType, data interface{}) {
		if txData, ok := data.(events.TxSubmittedData); ok {
			ws.server.BroadcastTxSubmitted(txData.TxHash, txData.Tx)
		}
	})

	// Subscribe to transaction confirmed events
	ws.eventBus.Subscribe(events.TxConfirmedEvent, func(eventType events.EventType, data interface{}) {
		if txData, ok := data.(events.TxConfirmedData); ok {
			ws.server.BroadcastTxConfirmed(txData.TxHash, txData.Tx, txData.Slot)
		}
	})

	// Subscribe to transaction failed events
	ws.eventBus.Subscribe(events.TxFailedEvent, func(eventType events.EventType, data interface{}) {
		if txData, ok := data.(events.TxFailedData); ok {
			ws.server.BroadcastTxFailed(txData.TxHash, txData.Tx, txData.Error)
		}
	})

	// Subscribe to block created events
	ws.eventBus.Subscribe(events.BlockCreatedEvent, func(eventType events.EventType, data interface{}) {
		if blockData, ok := data.(events.BlockCreatedData); ok {
			ws.server.BroadcastBlockCreated(blockData.Slot, blockData.TxHashes)
		}
	})

	// Subscribe to mempool stats events
	ws.eventBus.Subscribe(events.MempoolStatsEvent, func(eventType events.EventType, data interface{}) {
		if statsData, ok := data.(events.MempoolStatsData); ok {
			ws.server.BroadcastMempoolStats(statsData.TxCount, statsData.TxHashes, statsData.MaxSize)
		}
	})
}

// Start periodic mempool stats reporter
func (ws *WSService) startMempoolStatsReporter() {
	ticker := time.NewTicker(5 * time.Second) // Report every 5 seconds
	defer ticker.Stop()

	for range ticker.C {
		if ws.server.GetClientCount() > 0 {
			ws.mempool.PublishMempoolStats()
		}
	}
}

// GetClientCount returns the number of connected WebSocket clients
func (ws *WSService) GetClientCount() int {
	return ws.server.GetClientCount()
}

// BroadcastCustomEvent allows broadcasting custom events
func (ws *WSService) BroadcastCustomEvent(eventType string, data interface{}) {
	ws.server.BroadcastEvent(eventType, data)
}
