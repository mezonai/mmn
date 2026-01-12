package jsonrpc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/logx"
	pb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/proto/protoconnect"
	"github.com/mezonai/mmn/security/ratelimit"
	"github.com/mezonai/mmn/security/validation"
	"github.com/mezonai/mmn/transaction"
	vtgrpc "github.com/planetscale/vtprotobuf/codec/grpc"
)

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

// TxServiceHandler implements protoconnect.TxServiceHandler
type TxServiceHandler struct {
	protoconnect.UnimplementedTxServiceHandler
	txSvc                  interfaces.TxService
	userContentRateLimiter *ratelimit.RateLimiter
}

func NewTxServiceHandler(txSvc interfaces.TxService, userContentRateLimiter *ratelimit.RateLimiter) *TxServiceHandler {
	return &TxServiceHandler{
		txSvc:                  txSvc,
		userContentRateLimiter: userContentRateLimiter,
	}
}

func (h *TxServiceHandler) AddTx(ctx context.Context, req *connect.Request[pb.SignedTxMsg]) (*connect.Response[pb.AddTxResponse], error) {
	p := req.Msg
	clientIP, _ := ctx.Value(validation.ClientIPKey).(string)

	// Rate limit for user content transactions
	if p.TxMsg != nil && p.TxMsg.Type == transaction.TxTypeUserContent {
		if h.userContentRateLimiter != nil && !h.userContentRateLimiter.IsAllowed(clientIP) {
			logx.Warn("SECURITY", "User content rate limit exceeded from IP:", clientIP)
			return connect.NewResponse(&pb.AddTxResponse{Ok: false, Error: "Too many requests for user content transactions"}), nil
		}
	}

	// Validate fields
	if p.TxMsg != nil {
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
				return connect.NewResponse(&pb.AddTxResponse{Ok: false, Error: err.Error()}), nil
			}
		}

		for fieldName, fieldValue := range longFields {
			if err := validation.ValidateLongTextLength(fieldName, fieldValue); err != nil {
				return connect.NewResponse(&pb.AddTxResponse{Ok: false, Error: err.Error()}), nil
			}
		}
	}

	resp, err := h.txSvc.AddTx(ctx, p)
	if err != nil {
		return connect.NewResponse(&pb.AddTxResponse{Ok: false, Error: err.Error()}), nil
	}
	return connect.NewResponse(resp), nil
}

func (h *TxServiceHandler) GetTxByHash(ctx context.Context, req *connect.Request[pb.GetTxByHashRequest]) (*connect.Response[pb.GetTxByHashResponse], error) {
	txHash := req.Msg.TxHash
	if err := validation.ValidateShortTextLength(validation.TxHashField, txHash); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	resp, err := h.txSvc.GetTxByHash(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *TxServiceHandler) GetPendingTransactions(ctx context.Context, req *connect.Request[pb.GetPendingTransactionsRequest]) (*connect.Response[pb.GetPendingTransactionsResponse], error) {
	resp, err := h.txSvc.GetPendingTransactions(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *TxServiceHandler) SubscribeTransactionStatus(ctx context.Context, req *connect.Request[pb.SubscribeTransactionStatusRequest], stream *connect.ServerStream[pb.TransactionStatusInfo]) error {
	// Not implemented
	return connect.NewError(connect.CodeUnimplemented, fmt.Errorf("streaming not implemented"))
}

// AccountServiceHandler implements protoconnect.AccountServiceHandler
type AccountServiceHandler struct {
	protoconnect.UnimplementedAccountServiceHandler
	acctSvc interfaces.AccountService
}

func NewAccountServiceHandler(acctSvc interfaces.AccountService) *AccountServiceHandler {
	return &AccountServiceHandler{
		acctSvc: acctSvc,
	}
}

func (h *AccountServiceHandler) GetAccount(ctx context.Context, req *connect.Request[pb.GetAccountRequest]) (*connect.Response[pb.GetAccountResponse], error) {
	if err := validation.ValidateShortTextLength(validation.AddressField, req.Msg.Address); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	resp, err := h.acctSvc.GetAccount(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *AccountServiceHandler) GetCurrentNonce(ctx context.Context, req *connect.Request[pb.GetCurrentNonceRequest]) (*connect.Response[pb.GetCurrentNonceResponse], error) {
	if err := validation.ValidateShortTextLength(validation.AddressField, req.Msg.Address); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	tag := req.Msg.Tag
	if tag != "latest" && tag != "pending" {
		return connect.NewResponse(&pb.GetCurrentNonceResponse{
			Address: req.Msg.Address,
			Nonce:   0,
			Tag:     tag,
			Error:   "invalid tag: must be 'latest' or 'pending'",
		}), nil
	}

	resp, err := h.acctSvc.GetCurrentNonce(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

// HealthServiceHandler implements protoconnect.HealthServiceHandler
type HealthServiceHandler struct {
	protoconnect.UnimplementedHealthServiceHandler
	healthSvc interfaces.HealthService
}

func NewHealthServiceHandler(healthSvc interfaces.HealthService) *HealthServiceHandler {
	return &HealthServiceHandler{
		healthSvc: healthSvc,
	}
}

func (h *HealthServiceHandler) Check(ctx context.Context, req *connect.Request[pb.Empty]) (*connect.Response[pb.HealthCheckResponse], error) {
	resp, err := h.healthSvc.Check(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (h *HealthServiceHandler) Watch(ctx context.Context, req *connect.Request[pb.Empty], stream *connect.ServerStream[pb.HealthCheckResponse]) error {
	// Not implemented
	return connect.NewError(connect.CodeUnimplemented, fmt.Errorf("streaming not implemented"))
}

func (s *Server) BuildHTTPHandler() http.Handler {
	mux := http.NewServeMux()

	// Create connect handler options (vtprotobuf codec)
	opts := []connect.HandlerOption{
		connect.WithCodec(&vtgrpc.Codec{}),
	}

	// Register service handlers
	txHandler := NewTxServiceHandler(s.txSvc, s.userContentRateLimiter)
	txPath, txHTTPHandler := protoconnect.NewTxServiceHandler(txHandler, opts...)
	mux.Handle(txPath, txHTTPHandler)

	acctHandler := NewAccountServiceHandler(s.acctSvc)
	acctPath, acctHTTPHandler := protoconnect.NewAccountServiceHandler(acctHandler, opts...)
	mux.Handle(acctPath, acctHTTPHandler)

	healthHandler := NewHealthServiceHandler(s.healthSvc)
	healthPath, healthHTTPHandler := protoconnect.NewHealthServiceHandler(healthHandler, opts...)
	mux.Handle(healthPath, healthHTTPHandler)

	// Wrap with middleware for CORS, rate limiting, and request size validation
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.setCORSHeaders(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Determine request body limit based on path
		bodyLimit := int64(validation.DefaultRequestBodyLimit)
		if _, ok := validation.LargeRequestMethods[r.URL.Path]; ok {
			bodyLimit = int64(validation.LargeRequestBodyLimit)
		}
		r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)

		// Rate limiting
		if s.rateLimiter != nil && r.Method == http.MethodPost && s.enableRateLimit {
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

			clientIP := extractClientIPFromRequest(r)
			ctx := context.WithValue(r.Context(), validation.ClientIPKey, clientIP)
			r = r.WithContext(ctx)
			logx.Debug("SECURITY", "Client IP:", clientIP, "Path:", r.URL.Path)
			if !s.rateLimiter.IsIPAllowed(clientIP) {
				logx.Warn("SECURITY", "IP rate limit exceeded:", clientIP, "Path:", r.URL.Path)
				http.Error(w, "Too many requests", http.StatusTooManyRequests)
				return
			}
			exception.SafeGo("TrackIPRequest", func() {
				s.rateLimiter.TrackIPRequest(clientIP)
			})
		}

		mux.ServeHTTP(w, r)
	})

	return h
}

func (s *Server) Start() {
	h := s.BuildHTTPHandler()
	http.Handle("/", h)
	exception.SafeGoWithPanic("StartConnectServer", func() {
		err := http.ListenAndServe(s.addr, nil)
		if err != nil {
			logx.Error("CONNECT SERVER", fmt.Sprintf("Failed to serve Connect server: %v", err))
			panic(err)
		}
	})
}

// SetCORSConfig allows configuring CORS settings
func (s *Server) SetCORSConfig(config *CORSConfig) {
	s.corsConfig = *config
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
