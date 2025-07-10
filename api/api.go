package api

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"mmn/mempool"
)

type TxReq struct {
	Data []byte `json:"data"`
}

type APIServer struct {
	Mempool    *mempool.Mempool
	ListenAddr string
}

func NewAPIServer(mp *mempool.Mempool, addr string) *APIServer {
	return &APIServer{
		Mempool:    mp,
		ListenAddr: addr,
	}
}

func (s *APIServer) Start() {
	http.HandleFunc("/tx", s.handleTx)
	fmt.Printf("Tx API listen on %s\n", s.ListenAddr)
	go http.ListenAndServe(s.ListenAddr, nil)
}

func (s *APIServer) handleTx(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Handling tx", r.Method)
	if r.Method != "POST" {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := ioutil.ReadAll(r.Body)
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
	ok := s.Mempool.AddTx(req.Data)
	if !ok {
		http.Error(w, "Mempool full", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("ok"))
}
