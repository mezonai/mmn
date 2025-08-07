package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"mmn/ledger"
	"mmn/mempool"
	"mmn/p2p"
)

// P2PInfo represents P2P network information
type P2PInfo struct {
	NodeID         string   `json:"node_id"`
	PeerCount      int      `json:"peer_count"`
	ConnectedPeers []string `json:"connected_peers"`
	ListenAddrs    []string `json:"listen_addresses"`
	IsBootstrap    bool     `json:"is_bootstrap"`
}

// NetworkStatus represents overall network status
type NetworkStatus struct {
	P2P       P2PInfo `json:"p2p"`
	Mempool   int     `json:"mempool_size"`
	NodeType  string  `json:"node_type"`
	Timestamp int64   `json:"timestamp"`
}

// Enhanced API server with P2P support
type EnhancedAPIServer struct {
	Mempool        *mempool.Mempool
	Ledger         *ledger.Ledger
	NetworkAdapter *p2p.NetworkAdapter
	ListenAddr     string
	IsBootstrap    bool
}

// NewEnhancedAPIServer creates an enhanced API server with P2P support
func NewEnhancedAPIServer(mp *mempool.Mempool, ledger *ledger.Ledger, adapter *p2p.NetworkAdapter, addr string, isBootstrap bool) *EnhancedAPIServer {
	return &EnhancedAPIServer{
		Mempool:        mp,
		Ledger:         ledger,
		NetworkAdapter: adapter,
		ListenAddr:     addr,
		IsBootstrap:    isBootstrap,
	}
}

// Start starts the enhanced API server
func (s *EnhancedAPIServer) Start() {
	// Existing endpoints
	http.HandleFunc("/txs", s.handleTxs)
	http.HandleFunc("/account", s.handleAccount)

	// New P2P endpoints
	http.HandleFunc("/p2p/info", s.handleP2PInfo)
	http.HandleFunc("/p2p/peers", s.handlePeers)
	http.HandleFunc("/p2p/status", s.handleNetworkStatus)
	http.HandleFunc("/consensus/info", s.handleConsensusInfo)

	// Health check
	http.HandleFunc("/health", s.handleHealth)

	fmt.Printf("Enhanced API server listening on %s\n", s.ListenAddr)
	fmt.Printf("Available endpoints:\n")
	fmt.Printf("  - POST/GET /txs - Transaction operations\n")
	fmt.Printf("  - GET /account - Account information\n")
	fmt.Printf("  - GET /p2p/info - P2P node information\n")
	fmt.Printf("  - GET /p2p/peers - Connected peers\n")
	fmt.Printf("  - GET /p2p/status - Network status\n")
	fmt.Printf("  - GET /consensus/info - Consensus information\n")
	fmt.Printf("  - GET /health - Health check\n")

	go http.ListenAndServe(s.ListenAddr, nil)
}

// handleP2PInfo returns P2P node information
func (s *EnhancedAPIServer) handleP2PInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info := P2PInfo{
		NodeID:         s.NetworkAdapter.GetNodeID(),
		PeerCount:      s.NetworkAdapter.GetPeerCount(),
		ConnectedPeers: s.NetworkAdapter.GetPeers(),
		ListenAddrs:    s.NetworkAdapter.GetListenAddresses(),
		IsBootstrap:    s.IsBootstrap,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// handlePeers returns list of connected peers
func (s *EnhancedAPIServer) handlePeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	peers := s.NetworkAdapter.GetPeers()
	response := map[string]interface{}{
		"peer_count": len(peers),
		"peers":      peers,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleNetworkStatus returns overall network status
func (s *EnhancedAPIServer) handleNetworkStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodeType := "regular"
	if s.IsBootstrap {
		nodeType = "bootstrap"
	}

	status := NetworkStatus{
		P2P: P2PInfo{
			NodeID:         s.NetworkAdapter.GetNodeID(),
			PeerCount:      s.NetworkAdapter.GetPeerCount(),
			ConnectedPeers: s.NetworkAdapter.GetPeers(),
			ListenAddrs:    s.NetworkAdapter.GetListenAddresses(),
			IsBootstrap:    s.IsBootstrap,
		},
		Mempool:   s.Mempool.Size(),
		NodeType:  nodeType,
		Timestamp: getCurrentTimestamp(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleConsensusInfo returns consensus-related information
func (s *EnhancedAPIServer) handleConsensusInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// TODO: Add actual consensus information
	info := map[string]interface{}{
		"node_id":      s.NetworkAdapter.GetNodeID(),
		"peer_count":   s.NetworkAdapter.GetPeerCount(),
		"mempool_size": s.Mempool.Size(),
		"role":         "validator", // TODO: Get actual role from validator
		"status":       "active",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// handleHealth returns health status
func (s *EnhancedAPIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	health := map[string]interface{}{
		"status":       "healthy",
		"peer_count":   s.NetworkAdapter.GetPeerCount(),
		"mempool_size": s.Mempool.Size(),
		"timestamp":    getCurrentTimestamp(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

// Legacy methods for backwards compatibility
func (s *EnhancedAPIServer) handleTxs(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement transaction handling with P2P broadcasting
	fmt.Fprintf(w, "Transaction handling - TODO: Implement with P2P")
}

func (s *EnhancedAPIServer) handleAccount(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement account queries
	fmt.Fprintf(w, "Account queries - TODO: Implement")
}

func getCurrentTimestamp() int64 {
	return 1735430400 // Placeholder timestamp
}
