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
	"github.com/mezonai/mmn/security/validation"
	"github.com/mezonai/mmn/transaction"
)

const (
	clientIPKey = "clientIP"
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
	TxHash    string `json:"tx_hash"`
}

type getTxByHashResponse struct {
	Error    string  `json:"error"`
	Tx       *txInfo `json:"tx"`
	Decimals uint32  `json:"decimals"`
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

type HealthCheckResponse struct {
	Status       int32  `json:"status"`
	NodeID       string `json:"node_id"`
	Timestamp    uint64 `json:"timestamp"`
	CurrentSlot  uint64 `json:"current_slot"`
	BlockHeight  uint64 `json:"block_height"`
	MempoolSize  uint64 `json:"mempool_size"`
	IsLeader     bool   `json:"is_leader"`
	IsFollower   bool   `json:"is_follower"`
	Version      string `json:"version"`
	Uptime       uint64 `json:"uptime"`
	ErrorMessage string `json:"error_message"`
}

// --- Server ---

type Server struct {
	addr                   string
	txSvc                  interfaces.TxService
	acctSvc                interfaces.AccountService
	healthSvc              interfaces.HealthService
	corsConfig             CORSConfig
	enableRateLimit        bool
	rateLimiter            *ratelimit.GlobalRateLimiter
	userContentRateLimiter *ratelimit.RateLimiter
}

type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	MaxAge         int
}

func NewServer(addr string, txSvc interfaces.TxService, acctSvc interfaces.AccountService, healthSvc interfaces.HealthService,
	rateLimiter *ratelimit.GlobalRateLimiter, userContentRateLimiter *ratelimit.RateLimiter, enableRateLimit bool) *Server {
	return &Server{
		addr:      addr,
		txSvc:     txSvc,
		acctSvc:   acctSvc,
		healthSvc: healthSvc,
		corsConfig: CORSConfig{
			AllowedOrigins: []string{},
			AllowedMethods: []string{},
			AllowedHeaders: []string{},
			MaxAge:         0,
		},
		rateLimiter:            rateLimiter,
		userContentRateLimiter: userContentRateLimiter,
		enableRateLimit:        enableRateLimit,
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

		if r.Method == http.MethodPost {
			ct := r.Header.Get("Content-Type")
			if !strings.HasPrefix(strings.ToLower(ct), "application/json") {
				http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
				return
			}
		}

		r.Body = http.MaxBytesReader(w, r.Body, validation.DefaultRequestBodyLimit)

		if s.rateLimiter != nil && r.Method == http.MethodPost && s.enableRateLimit {
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

			clientIP := extractClientIPFromRequest(r)
			ctx := context.WithValue(r.Context(), clientIPKey, clientIP)
			r = r.WithContext(ctx)
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
	exception.SafeGoWithPanic("StartJSONRPCServer", func() {
		err := http.ListenAndServe(s.addr, nil)
		if err != nil {
			logx.Error("JSONRPC SERVER", fmt.Sprintf("Failed to serve JSON-RPC server: %v", err))
			panic(err)
		}
	})
}

// SetCORSConfig allows configuring CORS settings
func (s *Server) SetCORSConfig(config *CORSConfig) {
	s.corsConfig = *config
}

// Build jrpc2 method map
func (s *Server) buildMethodMap() handler.Map {
	return handler.Map{
		MethodTxAddTx: handler.New(func(ctx context.Context, p signedTxParams) (*addTxResponse, error) {
			clientIP, _ := ctx.Value(clientIPKey).(string)
			res := s.rpcAddTx(&p, clientIP)
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
		MethodHealthCheck: handler.New(func(ctx context.Context) (*HealthCheckResponse, error) {
			res, err := s.rpcHealthCheck(ctx)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*HealthCheckResponse), nil
		}),
	}
}

// Legacy routing removed; jrpc2 handler map is used instead

// --- Implementations ---

func (s *Server) rpcAddTx(p *signedTxParams, clientIP string) interface{} {
	shortFields := map[string]string{
		validation.SenderField:    p.TxMsg.Sender,
		validation.RecipientField: p.TxMsg.Recipient,
		validation.AmountField:    p.TxMsg.Amount,
	}

	longFields := map[string]string{
		validation.TextDataField:  p.TxMsg.TextData,
		validation.ExtraInfoField: p.TxMsg.ExtraInfo,
		validation.ZkProofField:   p.TxMsg.ZkProof,
		validation.ZkPubField:     p.TxMsg.ZkPub,
		validation.SignatureField: p.Signature,
	}

	for fieldName, fieldValue := range shortFields {
		if err := validation.ValidateShortTextLength(fieldName, fieldValue); err != nil {
			return &addTxResponse{Ok: false, Error: err.Error()}
		}
	}

	for fieldName, fieldValue := range longFields {
		if err := validation.ValidateLongTextLength(fieldName, fieldValue); err != nil {
			return &addTxResponse{Ok: false, Error: err.Error()}
		}
	}

	if p.TxMsg.Type == transaction.TxTypeUserContent {
		if !s.userContentRateLimiter.IsAllowed(clientIP) {
			logx.Warn("SECURITY", "User content rate limit exceeded from IP:", clientIP)
			return &addTxResponse{Ok: false, Error: "Too many requests for user content transactions"}
		}
	}

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
		return &addTxResponse{Ok: false, Error: err.Error()}
	}
	return &addTxResponse{Ok: resp.Ok, TxHash: resp.TxHash, Error: resp.Error}
}

func (s *Server) rpcGetTxByHash(p getTxByHashRequest) (interface{}, *rpcError) {
	txHash := p.TxHash
	if err := validation.ValidateShortTextLength(validation.TxHashField, txHash); err != nil {
		return nil, &rpcError{Code: -32602, Message: err.Error()}
	}

	resp, err := s.txSvc.GetTxByHash(context.Background(), &pb.GetTxByHashRequest{TxHash: txHash})
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
		Status:    int32(resp.Tx.Status),
		ErrMsg:    resp.Tx.ErrMsg,
		ExtraInfo: resp.Tx.ExtraInfo,
		TxHash:    resp.Tx.TxHash,
	}
	return &getTxByHashResponse{Tx: info, Decimals: resp.Decimals}, nil
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
	accAddr := p.Address
	if err := validation.ValidateShortTextLength(validation.AddressField, accAddr); err != nil {
		return nil, &rpcError{Code: -32602, Message: err.Error()}
	}

	resp, err := s.acctSvc.GetAccount(context.Background(), &pb.GetAccountRequest{Address: accAddr})
	if err != nil {
		return nil, &rpcError{Code: -32000, Message: err.Error()}
	}
	return &getAccountResponse{Address: resp.Address, Balance: resp.Balance, Nonce: resp.Nonce, Decimals: resp.Decimals}, nil
}

func (s *Server) rpcGetCurrentNonce(p getCurrentNonceRequest) (interface{}, *rpcError) {
	accAddr := p.Address
	tag := p.Tag

	if err := validation.ValidateShortTextLength(validation.AddressField, accAddr); err != nil {
		return nil, &rpcError{Code: -32602, Message: err.Error()}
	}

	if tag != "latest" && tag != "pending" {
		return &getCurrentNonceResponse{Address: accAddr, Nonce: 0, Tag: tag, Error: "invalid tag: must be 'latest' or 'pending'"}, nil
	}
	resp, err := s.acctSvc.GetCurrentNonce(context.Background(), &pb.GetCurrentNonceRequest{Address: accAddr, Tag: tag})
	if err != nil {
		return nil, &rpcError{Code: -32000, Message: err.Error()}
	}
	return &getCurrentNonceResponse{Address: resp.Address, Nonce: resp.Nonce, Tag: resp.Tag, Error: resp.Error}, nil
}

func (s *Server) rpcHealthCheck(ctx context.Context) (interface{}, *rpcError) {
	resp, err := s.healthSvc.Check(ctx)
	if err != nil {
		return nil, &rpcError{Code: -32000, Message: err.Error()}
	}
	return &HealthCheckResponse{Status: int32(resp.Status), NodeID: resp.NodeId, Timestamp: resp.Timestamp, CurrentSlot: resp.CurrentSlot, BlockHeight: resp.BlockHeight, MempoolSize: resp.MempoolSize, IsLeader: resp.IsLeader, IsFollower: resp.IsFollower, Version: resp.Version, Uptime: resp.Uptime, ErrorMessage: resp.ErrorMessage}, nil
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
