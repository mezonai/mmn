package faucet

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/logx"
)

// MultisigFaucetAPI provides HTTP API for multisig faucet operations
type MultisigFaucetAPI struct {
	service *MultisigFaucetService
}

// NewMultisigFaucetAPI creates a new multisig faucet API
func NewMultisigFaucetAPI(service *MultisigFaucetService) *MultisigFaucetAPI {
	return &MultisigFaucetAPI{
		service: service,
	}
}

// RegisterMultisigRequest represents a request to register a multisig configuration
type RegisterMultisigRequest struct {
	Threshold int      `json:"threshold"`
	Signers   []string `json:"signers"`
}

// RegisterMultisigResponse represents the response to register multisig request
type RegisterMultisigResponse struct {
	Success bool           `json:"success"`
	Config  *MultisigConfig `json:"config,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// CreateFaucetRequest represents a request to create a faucet transaction
type CreateFaucetRequest struct {
	MultisigAddress string `json:"multisig_address"`
	Recipient       string `json:"recipient"`
	Amount          string `json:"amount"`
	TextData        string `json:"text_data,omitempty"`
}

// CreateFaucetResponse represents the response to create faucet request
type CreateFaucetResponse struct {
	Success bool        `json:"success"`
	Tx      *MultisigTx `json:"tx,omitempty"`
	TxHash  string      `json:"tx_hash,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// AddSignatureRequest represents a request to add a signature
type AddSignatureRequest struct {
	TxHash        string `json:"tx_hash"`
	SignerPubKey  string `json:"signer_pub_key"`
	PrivateKey    string `json:"private_key"` // base58 encoded private key
}

// AddSignatureResponse represents the response to add signature request
type AddSignatureResponse struct {
	Success         bool `json:"success"`
	IsComplete      bool `json:"is_complete"`
	SignatureCount  int  `json:"signature_count"`
	RequiredCount   int  `json:"required_count"`
	Error           string `json:"error,omitempty"`
}

// ExecuteTransactionRequest represents a request to execute a transaction
type ExecuteTransactionRequest struct {
	TxHash string `json:"tx_hash"`
}

// ExecuteTransactionResponse represents the response to execute transaction request
type ExecuteTransactionResponse struct {
	Success bool        `json:"success"`
	Tx      *MultisigTx `json:"tx,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// GetTransactionStatusResponse represents the response to get transaction status
type GetTransactionStatusResponse struct {
	Success bool              `json:"success"`
	Status  *TransactionStatus `json:"status,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// GetServiceStatsResponse represents the response to get service stats
type GetServiceStatsResponse struct {
	Success bool          `json:"success"`
	Stats   *ServiceStats `json:"stats,omitempty"`
	Error   string        `json:"error,omitempty"`
}

// RegisterMultisig handles registration of a new multisig configuration
func (api *MultisigFaucetAPI) RegisterMultisig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RegisterMultisigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.writeErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	config, err := CreateMultisigConfig(req.Threshold, req.Signers)
	if err != nil {
		api.writeErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := api.service.RegisterMultisigConfig(config); err != nil {
		api.writeErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := RegisterMultisigResponse{
		Success: true,
		Config:  config,
	}

	api.writeJSONResponse(w, response, http.StatusOK)
}

// CreateFaucet handles creation of a new faucet request
func (api *MultisigFaucetAPI) CreateFaucet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateFaucetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.writeErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Parse amount
	amount, err := uint256.FromDecimal(req.Amount)
	if err != nil {
		api.writeErrorResponse(w, "Invalid amount format", http.StatusBadRequest)
		return
	}

	tx, err := api.service.CreateFaucetRequest(req.MultisigAddress, req.Recipient, amount, req.TextData)
	if err != nil {
		api.writeErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	txHash := tx.Hash()
	response := CreateFaucetResponse{
		Success: true,
		Tx:      tx,
		TxHash:  txHash,
	}

	api.writeJSONResponse(w, response, http.StatusOK)
}

// AddSignature handles adding a signature to a multisig transaction
func (api *MultisigFaucetAPI) AddSignature(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AddSignatureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.writeErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Decode private key
	privKey, err := api.decodePrivateKey(req.PrivateKey)
	if err != nil {
		api.writeErrorResponse(w, "Invalid private key format", http.StatusBadRequest)
		return
	}

	if err := api.service.AddSignature(req.TxHash, req.SignerPubKey, privKey); err != nil {
		api.writeErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get updated transaction status
	tx, err := api.service.GetPendingTransaction(req.TxHash)
	if err != nil {
		api.writeErrorResponse(w, "Failed to get transaction status", http.StatusInternalServerError)
		return
	}

	response := AddSignatureResponse{
		Success:        true,
		IsComplete:     tx.IsComplete(),
		SignatureCount: tx.GetSignatureCount(),
		RequiredCount:  tx.GetRequiredSignatureCount(),
	}

	api.writeJSONResponse(w, response, http.StatusOK)
}

// ExecuteTransaction handles execution of a verified multisig transaction
func (api *MultisigFaucetAPI) ExecuteTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ExecuteTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.writeErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tx, err := api.service.VerifyAndExecute(req.TxHash)
	if err != nil {
		api.writeErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	response := ExecuteTransactionResponse{
		Success: true,
		Tx:      tx,
	}

	api.writeJSONResponse(w, response, http.StatusOK)
}

// GetTransactionStatus handles getting the status of a transaction
func (api *MultisigFaucetAPI) GetTransactionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	txHash := r.URL.Query().Get("tx_hash")
	if txHash == "" {
		api.writeErrorResponse(w, "Missing tx_hash parameter", http.StatusBadRequest)
		return
	}

	status, err := api.service.GetTransactionStatus(txHash)
	if err != nil {
		api.writeErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	response := GetTransactionStatusResponse{
		Success: true,
		Status:  status,
	}

	api.writeJSONResponse(w, response, http.StatusOK)
}

// GetServiceStats handles getting service statistics
func (api *MultisigFaucetAPI) GetServiceStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := api.service.GetServiceStats()
	response := GetServiceStatsResponse{
		Success: true,
		Stats:   stats,
	}

	api.writeJSONResponse(w, response, http.StatusOK)
}

// ListPendingTransactions handles listing all pending transactions
func (api *MultisigFaucetAPI) ListPendingTransactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	transactions := api.service.ListPendingTransactions()
	
	response := struct {
		Success      bool          `json:"success"`
		Transactions []*MultisigTx `json:"transactions"`
		Count        int           `json:"count"`
	}{
		Success:      true,
		Transactions: transactions,
		Count:        len(transactions),
	}

	api.writeJSONResponse(w, response, http.StatusOK)
}

// Helper methods

func (api *MultisigFaucetAPI) writeJSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logx.Error("MultisigFaucetAPI", "failed to encode JSON response", err)
	}
}

func (api *MultisigFaucetAPI) writeErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	response := map[string]interface{}{
		"success": false,
		"error":   message,
	}
	api.writeJSONResponse(w, response, statusCode)
}

func (api *MultisigFaucetAPI) decodePrivateKey(privKeyStr string) ([]byte, error) {
	// This is a simplified implementation
	// In production, you should use proper key derivation and validation
	decoded, err := api.base58Decode(privKeyStr)
	if err != nil {
		return nil, err
	}
	
	// Validate private key length
	if len(decoded) != 32 && len(decoded) != 64 {
		return nil, fmt.Errorf("invalid private key length: %d", len(decoded))
	}
	
	return decoded, nil
}

func (api *MultisigFaucetAPI) base58Decode(str string) ([]byte, error) {
	// This is a placeholder - you should use the actual base58 implementation
	// from your codebase (likely from common package)
	return []byte(str), nil
}

// SetupRoutes sets up HTTP routes for the multisig faucet API
func (api *MultisigFaucetAPI) SetupRoutes() *http.ServeMux {
	mux := http.NewServeMux()
	
	mux.HandleFunc("/multisig/register", api.RegisterMultisig)
	mux.HandleFunc("/multisig/faucet/create", api.CreateFaucet)
	mux.HandleFunc("/multisig/faucet/sign", api.AddSignature)
	mux.HandleFunc("/multisig/faucet/execute", api.ExecuteTransaction)
	mux.HandleFunc("/multisig/faucet/status", api.GetTransactionStatus)
	mux.HandleFunc("/multisig/faucet/stats", api.GetServiceStats)
	mux.HandleFunc("/multisig/faucet/pending", api.ListPendingTransactions)
	
	return mux
}
