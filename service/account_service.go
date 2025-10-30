package service

import (
	"context"
	"fmt"

	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	pb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/utils"
)

type AccountServiceImpl struct {
	ledger     *ledger.Ledger
	blockStore store.BlockStore
	tracker    interfaces.TransactionTrackerInterface
}

func NewAccountService(ld *ledger.Ledger, bs store.BlockStore, tracker interfaces.TransactionTrackerInterface) *AccountServiceImpl {
	return &AccountServiceImpl{ledger: ld, blockStore: bs, tracker: tracker}
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

	// Get account from blockstore, get the latest finalized slot
	currentNonce := s.blockStore.GetLatestFinalizedSlot()

	return &pb.GetCurrentNonceResponse{
		Address: addr,
		Nonce:   currentNonce - 1, // Temporary fix to get the correct nonce
		Tag:     tag,
	}, nil
}
