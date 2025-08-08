package websocket

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"mmn/types"

	"github.com/gorilla/websocket"
)

// Event types
const (
	EventTypeTxSubmitted  = "tx_submitted"
	EventTypeTxConfirmed  = "tx_confirmed"
	EventTypeTxFailed     = "tx_failed"
	EventTypeBlockCreated = "block_created"
	EventTypeMempoolStats = "mempool_stats"
)

// WebSocket event structure
type Event struct {
	Type      string      `json:"type"`
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// Transaction event data
type TxEventData struct {
	TxHash string             `json:"tx_hash"`
	TxData *types.Transaction `json:"tx_data"`
	Slot   uint64             `json:"slot,omitempty"`
	Error  string             `json:"error,omitempty"`
}

// Block event data
type BlockEventData struct {
	Slot         uint64   `json:"slot"`
	TxCount      int      `json:"tx_count"`
	Transactions []string `json:"transactions"`
}

// Mempool stats data
type MempoolStatsData struct {
	TxCount  int      `json:"tx_count"`
	TxHashes []string `json:"tx_hashes"`
	MaxSize  int      `json:"max_size"`
	IsFull   bool     `json:"is_full"`
}

// WebSocket client connection
type Client struct {
	conn   *websocket.Conn
	send   chan Event
	server *Server
	id     string
}

// WebSocket server
type Server struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan Event
	upgrader   websocket.Upgrader
	mu         sync.RWMutex
}

// Create new WebSocket server
func NewServer() *Server {
	return &Server{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan Event, 256),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for now
			},
		},
	}
}

// Start the WebSocket server
func (s *Server) Start(addr string) {
	go s.run()

	http.HandleFunc("/ws", s.handleWebSocket)
	http.HandleFunc("/health", s.handleHealth)

	log.Printf("WebSocket server starting on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal("WebSocket server failed to start:", err)
	}
}

// Main server loop
func (s *Server) run() {
	for {
		select {
		case client := <-s.register:
			s.mu.Lock()
			s.clients[client] = true
			s.mu.Unlock()
			log.Printf("Client %s connected, total clients: %d", client.id, len(s.clients))

		case client := <-s.unregister:
			s.mu.Lock()
			if _, ok := s.clients[client]; ok {
				delete(s.clients, client)
				close(client.send)
			}
			s.mu.Unlock()
			log.Printf("Client %s disconnected, total clients: %d", client.id, len(s.clients))

		case event := <-s.broadcast:
			s.mu.RLock()
			for client := range s.clients {
				select {
				case client.send <- event:
				default:
					delete(s.clients, client)
					close(client.send)
				}
			}
			s.mu.RUnlock()
		}
	}
}

// Handle WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	clientID := fmt.Sprintf("client_%d", time.Now().UnixNano())
	client := &Client{
		conn:   conn,
		send:   make(chan Event, 256),
		server: s,
		id:     clientID,
	}

	s.register <- client

	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump()
}

// Health check endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	clientCount := len(s.clients)
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"clients":   clientCount,
		"timestamp": time.Now().Unix(),
	})
}

// Broadcast event to all connected clients
func (s *Server) BroadcastEvent(eventType string, data interface{}) {
	event := Event{
		Type:      eventType,
		Timestamp: time.Now().Unix(),
		Data:      data,
	}

	select {
	case s.broadcast <- event:
	default:
		log.Println("Warning: Broadcast channel is full, dropping event")
	}
}

// Broadcast transaction submitted event
func (s *Server) BroadcastTxSubmitted(txHash string, tx *types.Transaction) {
	s.BroadcastEvent(EventTypeTxSubmitted, TxEventData{
		TxHash: txHash,
		TxData: tx,
	})
}

// Broadcast transaction confirmed event
func (s *Server) BroadcastTxConfirmed(txHash string, tx *types.Transaction, slot uint64) {
	s.BroadcastEvent(EventTypeTxConfirmed, TxEventData{
		TxHash: txHash,
		TxData: tx,
		Slot:   slot,
	})
}

// Broadcast transaction failed event
func (s *Server) BroadcastTxFailed(txHash string, tx *types.Transaction, errorMsg string) {
	s.BroadcastEvent(EventTypeTxFailed, TxEventData{
		TxHash: txHash,
		TxData: tx,
		Error:  errorMsg,
	})
}

// Broadcast block created event
func (s *Server) BroadcastBlockCreated(slot uint64, txHashes []string) {
	s.BroadcastEvent(EventTypeBlockCreated, BlockEventData{
		Slot:         slot,
		TxCount:      len(txHashes),
		Transactions: txHashes,
	})
}

// Broadcast mempool stats event
func (s *Server) BroadcastMempoolStats(txCount int, txHashes []string, maxSize int) {
	s.BroadcastEvent(EventTypeMempoolStats, MempoolStatsData{
		TxCount:  txCount,
		TxHashes: txHashes,
		MaxSize:  maxSize,
		IsFull:   txCount >= maxSize,
	})
}

// Client write pump
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case event, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteJSON(event); err != nil {
				log.Printf("Error writing to client %s: %v", c.id, err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Client read pump
func (c *Client) readPump() {
	defer func() {
		c.server.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error for client %s: %v", c.id, err)
			}
			break
		}
	}
}

// Get connected clients count
func (s *Server) GetClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}
