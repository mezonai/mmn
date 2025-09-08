package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/utils"
)

type TxReq struct {
	Data []byte `json:"data"`
}

type APIServer struct {
	Mempool    *mempool.Mempool
	Ledger     *ledger.Ledger
	ListenAddr string
	// Faucet protection
	FaucetIPLimiter     *rateLimiter
	FaucetWalletLimiter *rateLimiter
	BlacklistedIPs      map[string]struct{}
}

func NewAPIServer(mp *mempool.Mempool, ledger *ledger.Ledger, addr string) *APIServer {
	// Faucet: per wallet cooldown 1 request / 5 minutes
	walletLimiter := newRateLimiter(1, 5*time.Minute)
	// Faucet: per IP cooldown 10 requests / hour
	ipLimiter := newRateLimiter(10, time.Hour)

	return &APIServer{
		Mempool:             mp,
		Ledger:              ledger,
		ListenAddr:          addr,
		FaucetIPLimiter:     ipLimiter,
		FaucetWalletLimiter: walletLimiter,
		BlacklistedIPs:      make(map[string]struct{}),
	}
}

func (s *APIServer) Start() {
	http.HandleFunc("/txs", s.handleTxs)
	http.HandleFunc("/account", s.handleAccount)
	http.HandleFunc("/faucet", s.handleFaucet)
	fmt.Printf("API listen on %s\n", s.ListenAddr)
	go http.ListenAndServe(s.ListenAddr, nil)
}

func (s *APIServer) handleTxs(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Handling txs", r.Method)
	switch r.Method {
	case http.MethodPost:
		s.submitTxHandler(w, r)
	case http.MethodGet:
		s.getTxsHandler(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *APIServer) submitTxHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		http.Error(w, "Empty body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req TxReq
	if err := json.Unmarshal(body, &req); err != nil || len(req.Data) == 0 {
		req.Data = body
		fmt.Println("Raw tx", string(req.Data))
	}
	tx, err := utils.ParseTx(req.Data)
	if err != nil {
		http.Error(w, "Invalid tx", http.StatusBadRequest)
		return
	}

	// Verify transaction signature
	if !tx.Verify() {
		http.Error(w, "Invalid signature", http.StatusBadRequest)
		return
	}

	_, err = s.Mempool.AddTx(tx, true)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to add transaction to mempool: %v", err), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("ok"))
}

func (s *APIServer) getTxsHandler(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("addr")
	if addr == "" {
		http.Error(w, "missing addr param", http.StatusBadRequest)
		return
	}
	limit, err := strconv.ParseUint(r.URL.Query().Get("limit"), 10, 32)
	if err != nil {
		limit = 10
	}
	offset, err := strconv.ParseUint(r.URL.Query().Get("offset"), 10, 32)
	if err != nil {
		offset = 0
	}
	filter, err := strconv.ParseUint(r.URL.Query().Get("filter"), 10, 32)
	if err != nil {
		filter = 0
	}

	fmt.Println("limit", limit, "offset", offset, "filter", filter)

	result := struct {
		Total uint32
		Txs   []*transaction.Transaction
	}{
		Total: 0,
		Txs:   make([]*transaction.Transaction, 0),
	}
	total, txs := s.Ledger.GetTxs(addr, uint32(limit), uint32(offset), uint32(filter))
	result.Total = total
	result.Txs = txs

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *APIServer) handleAccount(w http.ResponseWriter, r *http.Request) {
	addr := r.URL.Query().Get("addr")
	if addr == "" {
		http.Error(w, "missing addr param", http.StatusBadRequest)
		return
	}

	account, err := s.Ledger.GetAccount(addr)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get account: %v", err), http.StatusInternalServerError)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(account)
}

type FaucetReq struct {
	Address string `json:"address"`
}

func (s *APIServer) handleFaucet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := "unknown"
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		clientIP = ip
	} else {
		clientIP = r.RemoteAddr
	}

	if _, banned := s.BlacklistedIPs[clientIP]; banned {
		logx.Warn("FAUCET", fmt.Sprintf("Blocked faucet request from blacklisted IP %s", clientIP))
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req FaucetReq
	if err := json.Unmarshal(body, &req); err != nil || req.Address == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if s.FaucetIPLimiter != nil && !s.FaucetIPLimiter.Allow(clientIP) {
		logx.Warn("FAUCET", fmt.Sprintf("Rate limit exceeded for IP %s (faucet)", clientIP))
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	if s.FaucetWalletLimiter != nil && !s.FaucetWalletLimiter.Allow(req.Address) {
		logx.Warn("FAUCET", fmt.Sprintf("Cooldown active for wallet %s (faucet)", req.Address[:8]))
		http.Error(w, "cooldown active", http.StatusTooManyRequests)
		return
	}

	logx.Info("FAUCET", fmt.Sprintf("Accepted faucet request: ip=%s wallet=%s", clientIP, req.Address[:8]))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type rateLimiter struct {
	max     int
	window  time.Duration
	mu      sync.Mutex
	entries map[string][]time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{max: max, window: window, entries: make(map[string][]time.Time)}
}

func (l *rateLimiter) Allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	arr := l.entries[key]
	// drop old
	kept := arr[:0]
	for _, t := range arr {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.entries[key] = kept
		return false
	}
	kept = append(kept, now)
	l.entries[key] = kept
	return true
}
