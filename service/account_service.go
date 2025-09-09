package service

import (
	"context"

	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/mempool"
	pb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/utils"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AccountServiceImpl struct {
	ledger  *ledger.Ledger
	mempool *mempool.Mempool
	tracker interfaces.TransactionTrackerInterface
}

func NewAccountService(ld *ledger.Ledger, mp *mempool.Mempool, tracker interfaces.TransactionTrackerInterface) *AccountServiceImpl {
	return &AccountServiceImpl{ledger: ld, mempool: mp, tracker: tracker}
}

func (s *AccountServiceImpl) GetAccount(ctx context.Context, in *pb.GetAccountRequest) (*pb.GetAccountResponse, error) {
	addr := in.Address
	acc, err := s.ledger.GetAccount(addr)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return &pb.GetAccountResponse{Address: addr, Balance: "0", Nonce: 0, Decimals: uint32(config.GetDecimalsFactor())}, nil
	}
	return &pb.GetAccountResponse{Address: addr, Balance: utils.Uint256ToString(acc.Balance), Nonce: acc.Nonce, Decimals: uint32(config.GetDecimalsFactor())}, nil
}

func (s *AccountServiceImpl) GetTxHistory(ctx context.Context, in *pb.GetTxHistoryRequest) (*pb.GetTxHistoryResponse, error) {
	addr := in.Address
	total, txs := s.ledger.GetTxs(addr, in.Limit, in.Offset, in.Filter)
	txMetas := make([]*pb.TxMeta, len(txs))
	for i, tx := range txs {
		amount := utils.Uint256ToString(tx.Amount)
		txMetas[i] = &pb.TxMeta{Sender: tx.Sender, Recipient: tx.Recipient, Amount: amount, Nonce: tx.Nonce, Timestamp: tx.Timestamp, Status: pb.TxMeta_CONFIRMED, ExtraInfo: tx.ExtraInfo}
	}
	return &pb.GetTxHistoryResponse{Total: total, Txs: txMetas, Decimals: uint32(config.GetDecimalsFactor())}, nil
}

func (s *AccountServiceImpl) GetCurrentNonce(ctx context.Context, in *pb.GetCurrentNonceRequest) (*pb.GetCurrentNonceResponse, error) {
	addr := in.Address
	tag := in.Tag
	if tag != "latest" && tag != "pending" {
		return &pb.GetCurrentNonceResponse{Error: "invalid tag: must be 'latest' or 'pending'"}, nil
	}
	acc, err := s.ledger.GetAccount(addr)
	if err != nil {
		return &pb.GetCurrentNonceResponse{Address: addr, Nonce: 0, Tag: tag, Error: err.Error()}, nil
	}
	if acc == nil {
		return &pb.GetCurrentNonceResponse{Address: addr, Nonce: 0, Tag: tag}, nil
	}
	current := acc.Nonce
	if tag == "pending" {
		ledgerNonce := acc.Nonce
		pendingNonce := s.mempool.GetLargestPendingNonce(addr)
		var processing uint64
		if s.tracker != nil {
			processing = s.tracker.GetLargestProcessingNonce(addr)
		}
		current = ledgerNonce
		if pendingNonce > current { current = pendingNonce }
		if processing > current { current = processing }
	}
	return &pb.GetCurrentNonceResponse{Address: addr, Nonce: current, Tag: tag}, nil
}

func (s *AccountServiceImpl) GetAccountByAddress(ctx context.Context, in *pb.GetAccountByAddressRequest) (*pb.GetAccountByAddressResponse, error) {
	if in == nil || in.Address == "" {
		return &pb.GetAccountByAddressResponse{Error: "empty address"}, nil
	}
	acc, err := s.ledger.GetAccount(in.Address)
	if err != nil {
		return &pb.GetAccountByAddressResponse{Error: err.Error()}, nil
	}
	if acc == nil {
		return nil, status.Errorf(codes.NotFound, "account %s not found", in.Address)
	}
	return &pb.GetAccountByAddressResponse{Account: &pb.AccountData{Address: acc.Address, Balance: utils.Uint256ToString(acc.Balance), Nonce: acc.Nonce}}, nil
}



