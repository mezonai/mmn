package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"mmn/ledger"
	"mmn/mempool"
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
	_, ok := s.Mempool.AddTx(req.Data, true)
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

	result := struct {
		Pending   []ledger.Transaction
		Confirmed []ledger.TxRecord
	}{
		Pending:   make([]ledger.Transaction, 0),
		Confirmed: make([]ledger.TxRecord, 0),
	}
	result.Confirmed = s.Ledger.GetTxs(addr)

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
