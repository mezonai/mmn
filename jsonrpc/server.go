package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"strings"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"
	"github.com/creachadair/jrpc2/jhttp"
	"github.com/mezonai/mmn/errors"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
	pb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/security/ratelimit"
)

// --- Error type used by handlers ---

type jsonRPCRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func toJRPC2Error(e *rpcError) error {
	if e == nil {
		return nil
	}
	var networkError errors.NetworkError
	err := jsonx.Unmarshal([]byte(e.Message), &networkError)
	if err == nil {
		return jrpc2.Errorf(jrpc2.Code(e.Code), "%s", networkError.Message).WithData(networkError)
	}
	return jrpc2.Errorf(jrpc2.Code(e.Code), "%s", e.Message)
}

// --- Params/Results mirroring proto messages ---

// Tx
type txMsgParams struct {
	Type      int32  `json:"type"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount    string `json:"amount"`
	Timestamp uint64 `json:"timestamp"`
	TextData  string `json:"text_data"`
	Nonce     uint64 `json:"nonce"`
	ExtraInfo string `json:"extra_info"`
	ZkProof   string `json:"zk_proof"`
	ZkPub     string `json:"zk_pub"`
}

type signedTxParams struct {
	TxMsg     txMsgParams `json:"tx_msg"`
	Signature string      `json:"signature"`
}

type addTxResponse struct {
	Ok     bool   `json:"ok"`
	TxHash string `json:"tx_hash"`
	Error  string `json:"error"`
}

type getTxByHashRequest struct {
	TxHash string `json:"tx_hash"`
}

type txInfo struct {
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount    string `json:"amount"`
	Timestamp uint64 `json:"timestamp"`
	TextData  string `json:"text_data"`
	Nonce     uint64 `json:"nonce"`
	Slot      uint64 `json:"slot"`
	BlockHash string `json:"blockhash"`
	Status    int32  `json:"status"`
	ErrMsg    string `json:"err_msg"`
	ExtraInfo string `json:"extra_info"`
}

type getTxByHashResponse struct {
	Error    string  `json:"error"`
	Tx       *txInfo `json:"tx"`
	Decimals uint32  `json:"decimals"`
}

type getTxStatusRequest struct {
	TxHash string `json:"tx_hash"`
}

type txStatusInfo struct {
	TxHash        string `json:"tx_hash"`
	Status        int32  `json:"status"`
	BlockSlot     uint64 `json:"block_slot"`
	BlockHash     string `json:"block_hash"`
	Confirmations uint64 `json:"confirmations"`
	ErrorMessage  string `json:"error_message"`
	Timestamp     uint64 `json:"timestamp"`
	ExtraInfo     string `json:"extra_info"`
}

type getPendingTxsResponse struct {
	TotalCount uint64            `json:"total_count"`
	PendingTxs []transactionData `json:"pending_txs"`
	Error      string            `json:"error"`
}

type transactionData struct {
	TxHash    string `json:"tx_hash"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount    string `json:"amount"`
	Nonce     uint64 `json:"nonce"`
	Timestamp uint64 `json:"timestamp"`
	Status    int32  `json:"status"`
	TextData  string `json:"text_data,omitempty"`
}

// Account
type getAccountRequest struct {
	Address string `json:"address"`
}

type getAccountResponse struct {
	Address  string `json:"address"`
	Balance  string `json:"balance"`
	Nonce    uint64 `json:"nonce"`
	Decimals uint32 `json:"decimals"`
}

type getCurrentNonceRequest struct {
	Address string `json:"address"`
	Tag     string `json:"tag"`
}

type getCurrentNonceResponse struct {
	Address string `json:"address"`
	Nonce   uint64 `json:"nonce"`
	Tag     string `json:"tag"`
	Error   string `json:"error"`
}

// --- Server ---

type Server struct {
	addr            string
	txSvc           interfaces.TxService
	acctSvc         interfaces.AccountService
	corsConfig      CORSConfig
	enableRateLimit bool
	rateLimiter     *ratelimit.GlobalRateLimiter
}

type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	MaxAge         int
}

func NewServer(addr string, txSvc interfaces.TxService, acctSvc interfaces.AccountService, rateLimiter *ratelimit.GlobalRateLimiter, enableRateLimit bool) *Server {
	return &Server{
		addr:    addr,
		txSvc:   txSvc,
		acctSvc: acctSvc,
		corsConfig: CORSConfig{
			AllowedOrigins: []string{},
			AllowedMethods: []string{},
			AllowedHeaders: []string{},
			MaxAge:         0,
		},
		rateLimiter:     rateLimiter,
		enableRateLimit: enableRateLimit,
	}
}

func (s *Server) BuildHTTPHandler() http.Handler {
	methods := s.buildMethodMap()
	jh := jhttp.NewBridge(methods, &jhttp.BridgeOptions{Server: &jrpc2.ServerOptions{}})

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.setCORSHeaders(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if s.rateLimiter != nil && r.Method == http.MethodPost && s.enableRateLimit {
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

			clientIP := extractClientIPFromRequest(r)
			req := parseJSONRPCRequest(bodyBytes)
			if req == nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			logx.Debug("SECURITY", "Client IP:", clientIP, "Method:", req.Method)
			if !s.rateLimiter.IsIPAllowed(clientIP) {
				logx.Warn("SECURITY", "IP rate limit exceeded:", clientIP, "Method:", req.Method)
				http.Error(w, "Too many requests", http.StatusTooManyRequests)
				return
			}
			exception.SafeGo("TrackIPRequest", func() {
				s.rateLimiter.TrackIPRequest(clientIP)
			})
		}
		jh.ServeHTTP(w, r)
	})
	return h
}

func (s *Server) Start() {
	h := s.BuildHTTPHandler()
	http.Handle("/", h)
	go http.ListenAndServe(s.addr, nil)
}

// SetCORSConfig allows configuring CORS settings
func (s *Server) SetCORSConfig(config CORSConfig) {
	s.corsConfig = config
}

// Build jrpc2 method map
func (s *Server) buildMethodMap() handler.Map {
	return handler.Map{
		MethodTxAddTx: handler.New(func(ctx context.Context, p signedTxParams) (*addTxResponse, error) {
			res, err := s.rpcAddTx(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*addTxResponse), nil
		}),
		MethodTxGetTxByHash: handler.New(func(ctx context.Context, p getTxByHashRequest) (*getTxByHashResponse, error) {
			res, err := s.rpcGetTxByHash(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*getTxByHashResponse), nil
		}),
		MethodTxGetTransactionStatus: handler.New(func(ctx context.Context, p getTxStatusRequest) (*txStatusInfo, error) {
			res, err := s.rpcGetTransactionStatus(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*txStatusInfo), nil
		}),
		MethodTxGetPendingTransactions: handler.New(func(ctx context.Context) (*getPendingTxsResponse, error) {
			res, err := s.rpcGetPendingTransactions()
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*getPendingTxsResponse), nil
		}),
		MethodAccountGetAccount: handler.New(func(ctx context.Context, p getAccountRequest) (*getAccountResponse, error) {
			res, err := s.rpcGetAccount(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*getAccountResponse), nil
		}),
		MethodAccountGetCurrentNonce: handler.New(func(ctx context.Context, p getCurrentNonceRequest) (*getCurrentNonceResponse, error) {
			res, err := s.rpcGetCurrentNonce(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*getCurrentNonceResponse), nil
		}),
	}
}

// Legacy routing removed; jrpc2 handler map is used instead

// --- Implementations ---

func (s *Server) rpcAddTx(p signedTxParams) (interface{}, *rpcError) {
	pbSigned := &pb.SignedTxMsg{
		TxMsg: &pb.TxMsg{
			Type:      p.TxMsg.Type,
			Sender:    p.TxMsg.Sender,
			Recipient: p.TxMsg.Recipient,
			Amount:    p.TxMsg.Amount,
			Timestamp: p.TxMsg.Timestamp,
			TextData:  p.TxMsg.TextData,
			Nonce:     p.TxMsg.Nonce,
			ExtraInfo: p.TxMsg.ExtraInfo,
			ZkProof:   p.TxMsg.ZkProof,
			ZkPub:     p.TxMsg.ZkPub,
		},
		Signature: p.Signature,
	}
	resp, err := s.txSvc.AddTx(context.Background(), pbSigned)
	if err != nil {
		return &addTxResponse{Ok: false, Error: err.Error()}, nil
	}
	return &addTxResponse{Ok: resp.Ok, TxHash: resp.TxHash, Error: resp.Error}, nil
}

func (s *Server) rpcGetTxByHash(p getTxByHashRequest) (interface{}, *rpcError) {
	resp, err := s.txSvc.GetTxByHash(context.Background(), &pb.GetTxByHashRequest{TxHash: p.TxHash})
	if err != nil {
		return nil, &rpcError{Code: -32000, Message: err.Error()}
	}
	if resp.Error != "" {
		return &getTxByHashResponse{Error: resp.Error}, nil
	}
	if resp.Tx == nil {
		return &getTxByHashResponse{Error: "not found"}, nil
	}
	info := &txInfo{
		Sender:    resp.Tx.Sender,
		Recipient: resp.Tx.Recipient,
		Amount:    resp.Tx.Amount,
		Timestamp: resp.Tx.Timestamp,
		TextData:  resp.Tx.TextData,
		Nonce:     resp.Tx.Nonce,
		Slot:      resp.Tx.Slot,
		BlockHash: resp.Tx.Blockhash,
		Status:    resp.Tx.Status,
		ErrMsg:    resp.Tx.ErrMsg,
		ExtraInfo: resp.Tx.ExtraInfo,
	}
	return &getTxByHashResponse{Tx: info, Decimals: resp.Decimals}, nil
}

func (s *Server) rpcGetTransactionStatus(p getTxStatusRequest) (interface{}, *rpcError) {
	resp, err := s.txSvc.GetTransactionStatus(context.Background(), &pb.GetTransactionStatusRequest{TxHash: p.TxHash})
	if err != nil {
		return nil, &rpcError{Code: -32004, Message: err.Error(), Data: p.TxHash}
	}
	return &txStatusInfo{
		TxHash:        resp.TxHash,
		Status:        int32(resp.Status),
		BlockSlot:     resp.BlockSlot,
		BlockHash:     resp.BlockHash,
		Confirmations: resp.Confirmations,
		ErrorMessage:  resp.ErrorMessage,
		Timestamp:     resp.Timestamp,
		ExtraInfo:     resp.ExtraInfo,
	}, nil
}

func (s *Server) rpcGetPendingTransactions() (interface{}, *rpcError) {
	resp, err := s.txSvc.GetPendingTransactions(context.Background(), &pb.GetPendingTransactionsRequest{})
	if err != nil {
		return nil, &rpcError{Code: -32000, Message: err.Error()}
	}
	var out []transactionData
	for _, t := range resp.PendingTxs {
		out = append(out, transactionData{
			TxHash:    t.TxHash,
			Sender:    t.Sender,
			Recipient: t.Recipient,
			Amount:    t.Amount,
			Nonce:     t.Nonce,
			Timestamp: t.Timestamp,
			Status:    int32(t.Status),
			TextData:  t.TextData,
		})
	}
	return &getPendingTxsResponse{TotalCount: resp.TotalCount, PendingTxs: out, Error: resp.Error}, nil
}

func (s *Server) rpcGetAccount(p getAccountRequest) (interface{}, *rpcError) {
	resp, err := s.acctSvc.GetAccount(context.Background(), &pb.GetAccountRequest{Address: p.Address})
	if err != nil {
		return nil, &rpcError{Code: -32000, Message: err.Error()}
	}
	return &getAccountResponse{Address: resp.Address, Balance: resp.Balance, Nonce: resp.Nonce, Decimals: resp.Decimals}, nil
}

func (s *Server) rpcGetCurrentNonce(p getCurrentNonceRequest) (interface{}, *rpcError) {
	if p.Tag != "latest" && p.Tag != "pending" {
		return &getCurrentNonceResponse{Address: p.Address, Nonce: 0, Tag: p.Tag, Error: "invalid tag: must be 'latest' or 'pending'"}, nil
	}
	resp, err := s.acctSvc.GetCurrentNonce(context.Background(), &pb.GetCurrentNonceRequest{Address: p.Address, Tag: p.Tag})
	if err != nil {
		return nil, &rpcError{Code: -32000, Message: err.Error()}
	}
	return &getCurrentNonceResponse{Address: resp.Address, Nonce: resp.Nonce, Tag: resp.Tag, Error: resp.Error}, nil
}

// --- Helpers ---

func (s *Server) setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	// Set allowed origins
	if len(s.corsConfig.AllowedOrigins) > 0 {
		if s.corsConfig.AllowedOrigins[0] == "*" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			// Check if the request origin is in the allowed list
			origin := r.Header.Get("Origin")
			for _, allowedOrigin := range s.corsConfig.AllowedOrigins {
				if origin == allowedOrigin {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					break
				}
			}
		}
	}

	// Set allowed methods
	if len(s.corsConfig.AllowedMethods) > 0 {
		methods := strings.Join(s.corsConfig.AllowedMethods, ", ")
		w.Header().Set("Access-Control-Allow-Methods", methods)
	}

	// Set allowed headers
	if len(s.corsConfig.AllowedHeaders) > 0 {
		headers := strings.Join(s.corsConfig.AllowedHeaders, ", ")
		w.Header().Set("Access-Control-Allow-Headers", headers)
	}

	// Set max age
	if s.corsConfig.MaxAge > 0 {
		w.Header().Set("Access-Control-Max-Age", fmt.Sprintf("%d", s.corsConfig.MaxAge))
	}
}

// Legacy writeError removed

// --- Env helpers ---

// CORSFromEnv reads environment variables and constructs a CORSConfig.
// Returns (cfg, true) if any CORS-related env var is set; otherwise (zero, false).
//
// Env vars:
// - CORS_ALLOWED_ORIGINS: comma-separated list
// - CORS_ALLOWED_METHODS: comma-separated list
// - CORS_ALLOWED_HEADERS: comma-separated list
// - CORS_MAX_AGE: integer seconds
func CORSFromEnv() (CORSConfig, bool) {
	origins := os.Getenv("CORS_ALLOWED_ORIGINS")
	methods := os.Getenv("CORS_ALLOWED_METHODS")
	headers := os.Getenv("CORS_ALLOWED_HEADERS")
	maxAgeStr := os.Getenv("CORS_MAX_AGE")

	var maxAge int
	if maxAgeStr != "" {
		if v, err := strconv.Atoi(maxAgeStr); err == nil {
			maxAge = v
		}
	}

	var allowedOrigins, allowedMethods, allowedHeaders []string
	if origins != "" {
		allowedOrigins = splitAndTrim(origins)
	}
	if methods != "" {
		allowedMethods = splitAndTrim(methods)
	}
	if headers != "" {
		allowedHeaders = splitAndTrim(headers)
	}

	provided := len(allowedOrigins) > 0 || len(allowedMethods) > 0 || len(allowedHeaders) > 0 || maxAge > 0
	if !provided {
		return CORSConfig{}, false
	}

	return CORSConfig{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: allowedMethods,
		AllowedHeaders: allowedHeaders,
		MaxAge:         maxAge,
	}, true
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
