package api

import (
	"encoding/json"
	"fmt"
	"io"
	"mmn/ledger"
	"mmn/mempool"
	"mmn/types"
	"mmn/utils"
	"net/http"
	"strconv"
)

type TxReq struct {
	Data []byte `json:"data"`
}

type APIServer struct {
	Mempool    *mempool.Mempool
	Ledger     *ledger.Ledger
	ListenAddr string
}

func NewAPIServer(mp *mempool.Mempool, ledger *ledger.Ledger, addr string) *APIServer {
	return &APIServer{
		Mempool:    mp,
		Ledger:     ledger,
		ListenAddr: addr,
	}
}

func (s *APIServer) Start() {
	http.HandleFunc("/txs", s.handleTxs)
	http.HandleFunc("/account", s.handleAccount)
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
	_, ok := s.Mempool.AddTx(tx, true)
	if !ok {
		http.Error(w, "Mempool full", http.StatusServiceUnavailable)
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
		Txs   []*types.Transaction
	}{
		Total: 0,
		Txs:   make([]*types.Transaction, 0),
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

	account := s.Ledger.GetAccount(addr)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(account)
}
