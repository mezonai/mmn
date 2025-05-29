// File: pkg/api/api.go
package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"cryptocypher/pkg/blockchain"
	"cryptocypher/pkg/contract"
)

// Server holds references to the blockchain, ledger, and peer list.
type Server struct {
	Blockchain      *blockchain.Blockchain
	Ledger          blockchain.Ledger
	PeerList        []string
	StartTime       time.Time
	DynamicRegistry *contract.DynamicRegistry
}

// NewServer creates a new API server instance.
func NewServer(bc *blockchain.Blockchain, ledger blockchain.Ledger, peers []string, dr *contract.DynamicRegistry) *Server {
	return &Server{
		Blockchain:      bc,
		Ledger:          ledger,
		PeerList:        peers,
		StartTime:       time.Now(),
		DynamicRegistry: dr,
	}
}

// getChainHandler returns the full blockchain.
func (s *Server) getChainHandler(w http.ResponseWriter, r *http.Request) {
	chainJSON, err := json.Marshal(s.Blockchain.Blocks)
	if err != nil {
		http.Error(w, "Error marshalling chain", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(chainJSON)
}

// getHeadersHandler returns only the block headers.
func (s *Server) getHeadersHandler(w http.ResponseWriter, r *http.Request) {
	headers := s.Blockchain.ExtractHeaders()
	headersJSON, err := json.Marshal(headers)
	if err != nil {
		http.Error(w, "Error marshalling headers", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(headersJSON)
}

// getLatestBlockHandler returns the most recent block.
func (s *Server) getLatestBlockHandler(w http.ResponseWriter, r *http.Request) {
	if len(s.Blockchain.Blocks) == 0 {
		http.Error(w, "Blockchain is empty", http.StatusNotFound)
		return
	}
	latest := s.Blockchain.Blocks[len(s.Blockchain.Blocks)-1]
	blockJSON, err := json.Marshal(latest)
	if err != nil {
		http.Error(w, "Error marshalling block", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(blockJSON)
}

// getBlockHandler returns a block based on the provided hash.
func (s *Server) getBlockHandler(w http.ResponseWriter, r *http.Request) {
	hash := r.URL.Query().Get("hash")
	if hash == "" {
		http.Error(w, "Missing hash parameter", http.StatusBadRequest)
		return
	}
	block, err := blockchain.GetBlockFromChain(s.Blockchain, hash)
	if err != nil {
		http.Error(w, "Block not found", http.StatusNotFound)
		return
	}
	blockJSON, err := json.Marshal(block)
	if err != nil {
		http.Error(w, "Error marshalling block", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(blockJSON)
}

// getSubBlocksHandler returns sub-blocks of a given block.
// Query parameter "hash" identifies the parent block.
func (s *Server) getSubBlocksHandler(w http.ResponseWriter, r *http.Request) {
	hash := r.URL.Query().Get("hash")
	if hash == "" {
		http.Error(w, "Missing hash parameter", http.StatusBadRequest)
		return
	}
	block, err := blockchain.GetBlockFromChain(s.Blockchain, hash)
	if err != nil {
		http.Error(w, "Block not found", http.StatusNotFound)
		return
	}
	subBlocksJSON, err := json.Marshal(block.SubBlocks)
	if err != nil {
		http.Error(w, "Error marshalling sub-blocks", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(subBlocksJSON)
}

// getBalanceHandler returns the balance for a given address.
func (s *Server) getBalanceHandler(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Query().Get("address")
	if address == "" {
		http.Error(w, "Missing address parameter", http.StatusBadRequest)
		return
	}
	balance := s.Ledger[address]
	resp := map[string]interface{}{
		"address": address,
		"balance": balance,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// submitTransactionHandler accepts and verifies a new transaction.
func (s *Server) submitTransactionHandler(w http.ResponseWriter, r *http.Request) {
	var tx blockchain.Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		http.Error(w, "Invalid transaction format", http.StatusBadRequest)
		return
	}

	// Verify the signature.
	// We assume tx.Sender holds the hex-encoded public key.
	pubKeyBytes, err := hex.DecodeString(tx.Sender)
	if err != nil {
		http.Error(w, "Invalid sender public key format", http.StatusBadRequest)
		return
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), pubKeyBytes)
	if x == nil || y == nil {
		http.Error(w, "Could not unmarshal sender public key", http.StatusBadRequest)
		return
	}
	ecdsaPubKey := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     x,
		Y:     y,
	}

	if !blockchain.VerifyTransactionSignature(&tx, ecdsaPubKey) {
		http.Error(w, "Invalid transaction signature", http.StatusBadRequest)
		return
	}

	// Process the transaction (e.g., add it to a transaction pool).
	// For demonstration, we simply print it.
	fmt.Printf("Received valid transaction: %+v\n", tx)
	w.WriteHeader(http.StatusAccepted)
}

// executeContractHandler executes a smart contract based on input parameters.
func (s *Server) executeContractHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ContractName string                 `json:"contract_name"`
		Method       string                 `json:"method"`
		Params       map[string]interface{} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}
	result, err := contract.ExecuteContract(req.ContractName, req.Method, req.Params)
	if err != nil {
		http.Error(w, fmt.Sprintf("Contract execution error: %v", err), http.StatusBadRequest)
		return
	}
	resp := map[string]interface{}{
		"result": result,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// getPeersHandler returns the current peer list.
func (s *Server) getPeersHandler(w http.ResponseWriter, r *http.Request) {
	peerJSON, err := json.Marshal(s.PeerList)
	if err != nil {
		http.Error(w, "Error marshalling peer list", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(peerJSON)
}

// addPeerHandler allows clients to add a new peer manually.
func (s *Server) addPeerHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Peer string `json:"peer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Peer == "" {
		http.Error(w, "Invalid peer data", http.StatusBadRequest)
		return
	}
	// Avoid duplicates and self.
	if req.Peer != "" && !contains(s.PeerList, req.Peer) {
		s.PeerList = append(s.PeerList, req.Peer)
		fmt.Printf("Peer %s added.\n", req.Peer)
	}
	w.WriteHeader(http.StatusAccepted)
}

// removePeerHandler allows clients to remove a peer.
func (s *Server) removePeerHandler(w http.ResponseWriter, r *http.Request) {
	peer := r.URL.Query().Get("peer")
	if peer == "" {
		http.Error(w, "Missing peer parameter", http.StatusBadRequest)
		return
	}
	removed := false
	newPeers := []string{}
	for _, p := range s.PeerList {
		if p != peer {
			newPeers = append(newPeers, p)
		} else {
			removed = true
		}
	}
	s.PeerList = newPeers
	if removed {
		fmt.Printf("Peer %s removed.\n", peer)
		w.WriteHeader(http.StatusOK)
	} else {
		http.Error(w, "Peer not found", http.StatusNotFound)
	}
}

// contractStateHandler returns the state of a given contract.
// For demonstration, this is a stub endpoint.
func (s *Server) contractStateHandler(w http.ResponseWriter, r *http.Request) {
	contractName := r.URL.Query().Get("contract")
	if contractName == "" {
		http.Error(w, "Missing contract parameter", http.StatusBadRequest)
		return
	}
	// For now, return a dummy state. In a real implementation,
	// you would query the contract's stored state.
	state := map[string]interface{}{
		"contract": contractName,
		"state":    "dummy state",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

// pruneHandler manually triggers blockchain pruning.
func (s *Server) pruneHandler(w http.ResponseWriter, r *http.Request) {
	// For example, keep only the last 50 blocks.
	if err := s.Blockchain.PruneAndArchive(50, "archive_manual"); err != nil {
		http.Error(w, fmt.Sprintf("Pruning error: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Pruning triggered successfully."))
}

// statusHandler returns basic node status.
func (s *Server) statusHandler(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(s.StartTime).String()
	status := map[string]interface{}{
		"uptime":         uptime,
		"block_height":   len(s.Blockchain.Blocks),
		"peer_count":     len(s.PeerList),
		"ledger_entries": len(s.Ledger),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// metricsHandler returns dummy metrics for demonstration.
func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	metrics := map[string]interface{}{
		"transactions_per_second": 5.0,
		"blocks_per_minute":       2.0,
		"cpu_usage_percent":       15.0,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// contains is a helper function to check if a slice contains a string.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// In pkg/api/api.go, add:
// deployContractHandler allows external developers to deploy a new contract.
func (s *Server) deployContractHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ContractName string `json:"contract_name"`
		Code         string `json:"code"` // Hex-encoded WASM bytecode, for example.
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	// Decode the code.
	code, err := hex.DecodeString(req.Code)
	if err != nil {
		http.Error(w, "Invalid code encoding", http.StatusBadRequest)
		return
	}

	// Create a contract definition.
	def := contract.ContractDefinition{
		Name: req.ContractName,
		Code: code,
	}

	// Register the contract dynamically.
	if err := s.DynamicRegistry.RegisterContract(def); err != nil {
		http.Error(w, fmt.Sprintf("Error registering contract: %v", err), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Contract deployed successfully"))
}

// StartServer starts the API server on the specified port.
func (s *Server) StartServer(port string) {
	http.HandleFunc("/chain", s.getChainHandler)
	http.HandleFunc("/headers", s.getHeadersHandler)
	http.HandleFunc("/block", s.getBlockHandler)
	http.HandleFunc("/latestBlock", s.getLatestBlockHandler)
	http.HandleFunc("/subblocks", s.getSubBlocksHandler)
	http.HandleFunc("/balance", s.getBalanceHandler)
	http.HandleFunc("/transaction", s.submitTransactionHandler)
	http.HandleFunc("/contract", s.executeContractHandler)
	http.HandleFunc("/peers", s.getPeersHandler)
	http.HandleFunc("/addPeer", s.addPeerHandler)
	http.HandleFunc("/removePeer", s.removePeerHandler)
	http.HandleFunc("/contractState", s.contractStateHandler)
	http.HandleFunc("/prune", s.pruneHandler)
	http.HandleFunc("/status", s.statusHandler)
	http.HandleFunc("/metrics", s.metricsHandler)
	http.HandleFunc("/deployContract", s.deployContractHandler)
	fmt.Printf("API server listening on port %s\n", port)
	http.ListenAndServe(":"+port, nil)
}
