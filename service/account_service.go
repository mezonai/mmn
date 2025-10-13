package service

import (
	"context"
	"fmt"

	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
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
		logx.Error("GRPC GET ACCOUNT", "Ledger error", err)
		return nil, err
	}
	if acc == nil {
		return &pb.GetAccountResponse{
			Address:  addr,
			Balance:  "0",
			Nonce:    0,
			Decimals: uint32(config.GetDecimalsFactor()),
		}, nil
	}
	balance := utils.Uint256ToString(acc.Balance)
	logx.Info("GRPC", fmt.Sprintf("GetAccount response for address: %s, nonce: %d, balance: %s", addr, acc.Nonce, balance))

	return &pb.GetAccountResponse{
		Address:  addr,
		Balance:  balance,
		Nonce:    acc.Nonce,
		Decimals: uint32(config.GetDecimalsFactor()),
	}, nil
}

func (s *AccountServiceImpl) GetCurrentNonce(ctx context.Context, in *pb.GetCurrentNonceRequest) (*pb.GetCurrentNonceResponse, error) {
	addr := in.Address
	tag := in.Tag
	logx.Info("GRPC", fmt.Sprintf("GetCurrentNonce request for address: %s, tag: %s", addr, tag))

	// Validate tag parameter
	if tag != "latest" && tag != "pending" {
		return &pb.GetCurrentNonceResponse{
			Error: "invalid tag: must be 'latest' or 'pending'",
		}, nil
	}

	// Get account from ledger
	// TODO: verify this segment
	acc, err := s.ledger.GetAccount(addr)
	if err != nil {
		logx.Error("GRPC", fmt.Sprintf("Failed to get account for address %s: %v", addr, err))
		return &pb.GetCurrentNonceResponse{
			Address: addr,
			Nonce:   0,
			Tag:     tag,
			Error:   err.Error(),
		}, nil
	}
	if acc == nil {
		logx.Warn("GRPC", fmt.Sprintf("Account not found for address: %s", addr))
		return &pb.GetCurrentNonceResponse{
			Address: addr,
			Nonce:   0,
			Tag:     tag,
		}, nil
	}

	var currentNonce uint64

	if tag == "latest" {
		// For "latest", return the current nonce from the most recent mined block
		currentNonce = acc.Nonce
		logx.Info("GRPC", fmt.Sprintf("Latest current nonce for %s: %d", addr, currentNonce))
	} else { // tag == "pending"
		// For "pending", return the largest nonce among ready transactions, processing transactions, current ledger nonce, and consecutive pending nonce
		ledgerNonce := acc.Nonce
		largestReadyNonce := s.mempool.GetLargestReadyTransactionNonce(addr)
		largestProcessingNonce := s.tracker.GetLargestProcessingNonce(addr)
		currentNonce = max(largestProcessingNonce, max(largestReadyNonce, ledgerNonce))

		currentNonce = s.mempool.GetLargestConsecutivePendingNonce(addr, currentNonce)
		logx.Info("GRPC", fmt.Sprintf("Pending current nonce for %s: ledger: %d, mempool: %d, processing: %d, final: %d",
			addr, ledgerNonce, largestReadyNonce, largestProcessingNonce, currentNonce))
	}

	return &pb.GetCurrentNonceResponse{
		Address: addr,
		Nonce:   currentNonce,
		Tag:     tag,
	}, nil
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
	return &pb.GetAccountByAddressResponse{
		Account: &pb.AccountData{Address: acc.Address, Balance: utils.Uint256ToString(acc.Balance), Nonce: acc.Nonce},
	}, nil
}
