package jsonrpc

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"strings"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"
	"github.com/creachadair/jrpc2/jhttp"
	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/errors"
	"github.com/mezonai/mmn/faucet"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/jsonx"
	pb "github.com/mezonai/mmn/proto"
)

// --- Error type used by handlers ---

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

// Multisig Faucet types
type createFaucetRequestParams struct {
	MultisigAddress string `json:"multisig_address"`
	Recipient       string `json:"recipient"`
	Amount          string `json:"amount"`
	TextData        string `json:"text_data"`
	SignerPubkey    string `json:"signer_pubkey"`
	Signature       string `json:"signature"`
	ZkProof         string `json:"zk_proof,omitempty"`
	ZkPub           string `json:"zk_pub,omitempty"`
}

type createFaucetRequestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	TxHash  string `json:"tx_hash"`
}

type addSignatureParams struct {
	TxHash       string `json:"tx_hash"`
	SignerPubkey string `json:"signer_pubkey"`
	Signature    string `json:"signature"`
	ZkProof      string `json:"zk_proof,omitempty"`
	ZkPub        string `json:"zk_pub,omitempty"`
}

type addSignatureResponse struct {
	Success        bool   `json:"success"`
	Message        string `json:"message"`
	SignatureCount int32  `json:"signature_count"`
}

type rejectProposalParams struct {
	TxHash       string `json:"tx_hash"`
	SignerPubkey string `json:"signer_pubkey"`
	Signature    string `json:"signature"`
	ZkProof      string `json:"zk_proof,omitempty"`
	ZkPub        string `json:"zk_pub,omitempty"`
}

type rejectProposalResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type getMultisigTxStatusParams struct {
	TxHash string `json:"tx_hash"`
}

type getMultisigTxStatusResponse struct {
	Success            bool   `json:"success"`
	Message            string `json:"message"`
	Status             string `json:"status"`
	SignatureCount     int32  `json:"signature_count"`
	RequiredSignatures int32  `json:"required_signatures"`
}

type addToApproverWhitelistParams struct {
	Address      string `json:"address"`
	SignerPubkey string `json:"signer_pubkey"`
	Signature    string `json:"signature"`
	ZkProof      string `json:"zk_proof,omitempty"`
	ZkPub        string `json:"zk_pub,omitempty"`
}

type addToApproverWhitelistResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type addToProposerWhitelistParams struct {
	Address      string `json:"address"`
	SignerPubkey string `json:"signer_pubkey"`
	Signature    string `json:"signature"`
	ZkProof      string `json:"zk_proof,omitempty"`
	ZkPub        string `json:"zk_pub,omitempty"`
}

type addToProposerWhitelistResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type removeFromApproverWhitelistParams struct {
	Address      string `json:"address"`
	SignerPubkey string `json:"signer_pubkey"`
	Signature    string `json:"signature"`
	ZkProof      string `json:"zk_proof,omitempty"`
	ZkPub        string `json:"zk_pub,omitempty"`
}

type removeFromApproverWhitelistResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type removeFromProposerWhitelistParams struct {
	Address      string `json:"address"`
	SignerPubkey string `json:"signer_pubkey"`
	Signature    string `json:"signature"`
	ZkProof      string `json:"zk_proof,omitempty"`
	ZkPub        string `json:"zk_pub,omitempty"`
}

type removeFromProposerWhitelistResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type checkWhitelistStatusParams struct {
	Address string `json:"address"`
}

type checkWhitelistStatusResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	IsApprover bool   `json:"is_approver"`
	IsProposer bool   `json:"is_proposer"`
}

type getApproverWhitelistResponse struct {
	Success   bool     `json:"success"`
	Message   string   `json:"message"`
	Addresses []string `json:"addresses"`
}

type getProposerWhitelistResponse struct {
	Success   bool     `json:"success"`
	Message   string   `json:"message"`
	Addresses []string `json:"addresses"`
}

type getPendingProposalsResponse struct {
	Success    bool     `json:"success"`
	Message    string   `json:"message"`
	TotalCount uint64   `json:"total_count"`
	PendingTxs []string `json:"pending_txs"`
}

// --- Server ---

type Server struct {
	addr              string
	txSvc             interfaces.TxService
	acctSvc           interfaces.AccountService
	multisigFaucetSvc *faucet.MultisigFaucetService
	corsConfig        CORSConfig
}

type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	MaxAge         int
}

func NewServer(addr string, txSvc interfaces.TxService, acctSvc interfaces.AccountService, multisigFaucetSvc *faucet.MultisigFaucetService) *Server {
	return &Server{
		addr:              addr,
		txSvc:             txSvc,
		acctSvc:           acctSvc,
		multisigFaucetSvc: multisigFaucetSvc,
		corsConfig: CORSConfig{
			AllowedOrigins: []string{},
			AllowedMethods: []string{},
			AllowedHeaders: []string{},
			MaxAge:         0,
		},
	}
}

func (s *Server) Start() {
	methods := s.buildMethodMap()
	jh := jhttp.NewBridge(methods, &jhttp.BridgeOptions{Server: &jrpc2.ServerOptions{}})

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.setCORSHeaders(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		jh.ServeHTTP(w, r)
	})

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
		"tx.addtx": handler.New(func(ctx context.Context, p signedTxParams) (*addTxResponse, error) {
			res, err := s.rpcAddTx(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*addTxResponse), nil
		}),
		"tx.gettxbyhash": handler.New(func(ctx context.Context, p getTxByHashRequest) (*getTxByHashResponse, error) {
			res, err := s.rpcGetTxByHash(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*getTxByHashResponse), nil
		}),
		"tx.gettransactionstatus": handler.New(func(ctx context.Context, p getTxStatusRequest) (*txStatusInfo, error) {
			res, err := s.rpcGetTransactionStatus(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*txStatusInfo), nil
		}),
		"tx.getpendingtransactions": handler.New(func(ctx context.Context) (*getPendingTxsResponse, error) {
			res, err := s.rpcGetPendingTransactions()
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*getPendingTxsResponse), nil
		}),
		"account.getaccount": handler.New(func(ctx context.Context, p getAccountRequest) (*getAccountResponse, error) {
			res, err := s.rpcGetAccount(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*getAccountResponse), nil
		}),
		"account.getcurrentnonce": handler.New(func(ctx context.Context, p getCurrentNonceRequest) (*getCurrentNonceResponse, error) {
			res, err := s.rpcGetCurrentNonce(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*getCurrentNonceResponse), nil
		}),
		// Multisig Faucet methods
		"faucet.createproposal": handler.New(func(ctx context.Context, p createFaucetRequestParams) (*createFaucetRequestResponse, error) {
			res, err := s.rpcCreateFaucetRequest(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*createFaucetRequestResponse), nil
		}),
		"faucet.approve": handler.New(func(ctx context.Context, p addSignatureParams) (*addSignatureResponse, error) {
			res, err := s.rpcAddSignature(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*addSignatureResponse), nil
		}),
		"faucet.reject": handler.New(func(ctx context.Context, p rejectProposalParams) (*rejectProposalResponse, error) {
			res, err := s.rpcRejectProposal(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*rejectProposalResponse), nil
		}),
		"faucet.getstatus": handler.New(func(ctx context.Context, p getMultisigTxStatusParams) (*getMultisigTxStatusResponse, error) {
			res, err := s.rpcGetMultisigTxStatus(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*getMultisigTxStatusResponse), nil
		}),
		"faucet.addapprover": handler.New(func(ctx context.Context, p addToApproverWhitelistParams) (*addToApproverWhitelistResponse, error) {
			res, err := s.rpcAddToApproverWhitelist(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*addToApproverWhitelistResponse), nil
		}),
		"faucet.addproposer": handler.New(func(ctx context.Context, p addToProposerWhitelistParams) (*addToProposerWhitelistResponse, error) {
			res, err := s.rpcAddToProposerWhitelist(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*addToProposerWhitelistResponse), nil
		}),
		"faucet.removeapprover": handler.New(func(ctx context.Context, p removeFromApproverWhitelistParams) (*removeFromApproverWhitelistResponse, error) {
			res, err := s.rpcRemoveFromApproverWhitelist(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*removeFromApproverWhitelistResponse), nil
		}),
		"faucet.removeproposer": handler.New(func(ctx context.Context, p removeFromProposerWhitelistParams) (*removeFromProposerWhitelistResponse, error) {
			res, err := s.rpcRemoveFromProposerWhitelist(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*removeFromProposerWhitelistResponse), nil
		}),
		"faucet.checkwhitelist": handler.New(func(ctx context.Context, p checkWhitelistStatusParams) (*checkWhitelistStatusResponse, error) {
			res, err := s.rpcCheckWhitelistStatus(p)
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*checkWhitelistStatusResponse), nil
		}),
		"faucet.listapprovers": handler.New(func(ctx context.Context) (*getApproverWhitelistResponse, error) {
			res, err := s.rpcGetApproverWhitelist()
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*getApproverWhitelistResponse), nil
		}),
		"faucet.listproposers": handler.New(func(ctx context.Context) (*getProposerWhitelistResponse, error) {
			res, err := s.rpcGetProposerWhitelist()
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*getProposerWhitelistResponse), nil
		}),
		"faucet.getproposals": handler.New(func(ctx context.Context) (*getPendingProposalsResponse, error) {
			res, err := s.rpcGetPendingProposals()
			if err != nil {
				return nil, toJRPC2Error(err)
			}
			if res == nil {
				return nil, nil
			}
			return res.(*getPendingProposalsResponse), nil
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

func (s *Server) rpcCreateFaucetRequest(p createFaucetRequestParams) (interface{}, *rpcError) {
	if s.multisigFaucetSvc == nil {
		return &createFaucetRequestResponse{Success: false, Message: "Multisig faucet service not initialized"}, nil
	}

	signature, err := hex.DecodeString(p.Signature)
	if err != nil {
		return &createFaucetRequestResponse{Success: false, Message: fmt.Sprintf("Invalid signature format: %v", err)}, nil
	}

	amount, err := uint256.FromDecimal(p.Amount)
	if err != nil {
		return &createFaucetRequestResponse{Success: false, Message: fmt.Sprintf("Invalid amount format: %v", err)}, nil
	}

	resp, err := s.multisigFaucetSvc.CreateFaucetRequest(
		p.MultisigAddress,
		amount,
		p.TextData,
		p.SignerPubkey,
		signature,
		p.ZkProof,
		p.ZkPub,
	)
	if err != nil {
		return &createFaucetRequestResponse{Success: false, Message: err.Error()}, nil
	}

	return &createFaucetRequestResponse{
		Success: true,
		Message: "Faucet request created successfully",
		TxHash:  resp.Hash(),
	}, nil
}

func (s *Server) rpcAddSignature(p addSignatureParams) (interface{}, *rpcError) {
	if s.multisigFaucetSvc == nil {
		return &addSignatureResponse{Success: false, Message: "Multisig faucet service not initialized"}, nil
	}

	signature, err := hex.DecodeString(p.Signature)
	if err != nil {
		return &addSignatureResponse{Success: false, Message: fmt.Sprintf("Invalid signature format: %v", err)}, nil
	}

	err = s.multisigFaucetSvc.AddSignature(p.TxHash, p.SignerPubkey, signature, p.ZkProof, p.ZkPub)
	if err != nil {
		return &addSignatureResponse{Success: false, Message: err.Error()}, nil
	}

	tx, err := s.multisigFaucetSvc.GetMultisigTx(p.TxHash)
	if err != nil {
		return &addSignatureResponse{Success: false, Message: err.Error()}, nil
	}

	return &addSignatureResponse{
		Success:        true,
		Message:        "Signature added successfully",
		SignatureCount: int32(len(tx.Signatures)),
	}, nil
}

func (s *Server) rpcRejectProposal(p rejectProposalParams) (interface{}, *rpcError) {
	if s.multisigFaucetSvc == nil {
		return &rejectProposalResponse{Success: false, Message: "Multisig faucet service not initialized"}, nil
	}

	signature, err := hex.DecodeString(p.Signature)
	if err != nil {
		return &rejectProposalResponse{Success: false, Message: fmt.Sprintf("Invalid signature format: %v", err)}, nil
	}

	err = s.multisigFaucetSvc.RejectProposal(p.TxHash, p.SignerPubkey, signature, p.ZkProof, p.ZkPub)
	if err != nil {
		return &rejectProposalResponse{Success: false, Message: err.Error()}, nil
	}

	return &rejectProposalResponse{
		Success: true,
		Message: "Proposal rejected successfully",
	}, nil
}

func (s *Server) rpcGetMultisigTxStatus(p getMultisigTxStatusParams) (interface{}, *rpcError) {
	if s.multisigFaucetSvc == nil {
		return &getMultisigTxStatusResponse{Success: false, Message: "Multisig faucet service not initialized"}, nil
	}

	tx, err := s.multisigFaucetSvc.GetMultisigTx(p.TxHash)
	if err != nil {
		return &getMultisigTxStatusResponse{Success: false, Message: err.Error()}, nil
	}

	status := tx.Status
	if status == "" {
		status = "pending"
	}

	return &getMultisigTxStatusResponse{
		Success:            true,
		Message:            tx.TextData,
		Status:             status,
		SignatureCount:     int32(len(tx.Signatures)),
		RequiredSignatures: int32(tx.Config.Threshold),
	}, nil
}

func (s *Server) rpcAddToApproverWhitelist(p addToApproverWhitelistParams) (interface{}, *rpcError) {
	if s.multisigFaucetSvc == nil {
		return &addToApproverWhitelistResponse{Success: false, Message: "Multisig faucet service not initialized"}, nil
	}

	signature, err := hex.DecodeString(p.Signature)
	if err != nil {
		return &addToApproverWhitelistResponse{Success: false, Message: fmt.Sprintf("Invalid signature format: %v", err)}, nil
	}

	err = s.multisigFaucetSvc.AddToApproverWhitelist(p.Address, p.SignerPubkey, signature, p.ZkProof, p.ZkPub)
	if err != nil {
		return &addToApproverWhitelistResponse{Success: false, Message: err.Error()}, nil
	}

	return &addToApproverWhitelistResponse{
		Success: true,
		Message: "Address added to approver whitelist successfully",
	}, nil
}

func (s *Server) rpcAddToProposerWhitelist(p addToProposerWhitelistParams) (interface{}, *rpcError) {
	if s.multisigFaucetSvc == nil {
		return &addToProposerWhitelistResponse{Success: false, Message: "Multisig faucet service not initialized"}, nil
	}

	signature, err := hex.DecodeString(p.Signature)
	if err != nil {
		return &addToProposerWhitelistResponse{Success: false, Message: fmt.Sprintf("Invalid signature format: %v", err)}, nil
	}

	err = s.multisigFaucetSvc.AddToProposerWhitelist(p.Address, p.SignerPubkey, signature, p.ZkProof, p.ZkPub)
	if err != nil {
		return &addToProposerWhitelistResponse{Success: false, Message: err.Error()}, nil
	}

	return &addToProposerWhitelistResponse{
		Success: true,
		Message: "Address added to proposer whitelist successfully",
	}, nil
}

func (s *Server) rpcRemoveFromApproverWhitelist(p removeFromApproverWhitelistParams) (interface{}, *rpcError) {
	if s.multisigFaucetSvc == nil {
		return &removeFromApproverWhitelistResponse{Success: false, Message: "Multisig faucet service not initialized"}, nil
	}

	signature, err := hex.DecodeString(p.Signature)
	if err != nil {
		return &removeFromApproverWhitelistResponse{Success: false, Message: fmt.Sprintf("Invalid signature format: %v", err)}, nil
	}

	err = s.multisigFaucetSvc.RemoveFromApproverWhitelist(p.Address, p.SignerPubkey, signature, p.ZkProof, p.ZkPub)
	if err != nil {
		return &removeFromApproverWhitelistResponse{Success: false, Message: err.Error()}, nil
	}

	return &removeFromApproverWhitelistResponse{
		Success: true,
		Message: "Address removed from approver whitelist successfully",
	}, nil
}

func (s *Server) rpcRemoveFromProposerWhitelist(p removeFromProposerWhitelistParams) (interface{}, *rpcError) {
	if s.multisigFaucetSvc == nil {
		return &removeFromProposerWhitelistResponse{Success: false, Message: "Multisig faucet service not initialized"}, nil
	}

	signature, err := hex.DecodeString(p.Signature)
	if err != nil {
		return &removeFromProposerWhitelistResponse{Success: false, Message: fmt.Sprintf("Invalid signature format: %v", err)}, nil
	}

	err = s.multisigFaucetSvc.RemoveFromProposerWhitelist(p.Address, p.SignerPubkey, signature, p.ZkProof, p.ZkPub)
	if err != nil {
		return &removeFromProposerWhitelistResponse{Success: false, Message: err.Error()}, nil
	}

	return &removeFromProposerWhitelistResponse{
		Success: true,
		Message: "Address removed from proposer whitelist successfully",
	}, nil
}

func (s *Server) rpcCheckWhitelistStatus(p checkWhitelistStatusParams) (interface{}, *rpcError) {
	if s.multisigFaucetSvc == nil {
		return &checkWhitelistStatusResponse{Success: false, Message: "Multisig faucet service not initialized"}, nil
	}

	isApprover := s.multisigFaucetSvc.IsApprover(p.Address)
	isProposer := s.multisigFaucetSvc.IsProposer(p.Address)

	return &checkWhitelistStatusResponse{
		Success:    true,
		Message:    "Whitelist status retrieved",
		IsApprover: isApprover,
		IsProposer: isProposer,
	}, nil
}

func (s *Server) rpcGetApproverWhitelist() (interface{}, *rpcError) {
	if s.multisigFaucetSvc == nil {
		return &getApproverWhitelistResponse{Success: false, Message: "Multisig faucet service not initialized"}, nil
	}

	addresses := s.multisigFaucetSvc.GetApproverWhitelist()

	return &getApproverWhitelistResponse{
		Success:   true,
		Message:   "Approver whitelist retrieved successfully",
		Addresses: addresses,
	}, nil
}

func (s *Server) rpcGetProposerWhitelist() (interface{}, *rpcError) {
	if s.multisigFaucetSvc == nil {
		return &getProposerWhitelistResponse{Success: false, Message: "Multisig faucet service not initialized"}, nil
	}

	addresses := s.multisigFaucetSvc.GetProposerWhitelist()

	return &getProposerWhitelistResponse{
		Success:   true,
		Message:   "Proposer whitelist retrieved successfully",
		Addresses: addresses,
	}, nil
}

func (s *Server) rpcGetPendingProposals() (interface{}, *rpcError) {
	if s.multisigFaucetSvc == nil {
		return &getPendingProposalsResponse{Success: false, Message: "Multisig faucet service not initialized"}, nil
	}

	pendingTxs := s.multisigFaucetSvc.GetPendingTxs()
	var txHashes []string
	for txHash := range pendingTxs {
		txHashes = append(txHashes, txHash)
	}

	return &getPendingProposalsResponse{
		Success:    true,
		Message:    "Pending proposals retrieved successfully",
		TotalCount: uint64(len(txHashes)),
		PendingTxs: txHashes,
	}, nil
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
