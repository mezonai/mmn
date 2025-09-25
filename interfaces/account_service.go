package interfaces

import (
	"context"

	pb "github.com/mezonai/mmn/proto"
)

type AccountService interface {
	GetAccount(ctx context.Context, in *pb.GetAccountRequest) (*pb.GetAccountResponse, error)
	GetCurrentNonce(ctx context.Context, in *pb.GetCurrentNonceRequest) (*pb.GetCurrentNonceResponse, error)
	GetAccountByAddress(ctx context.Context, in *pb.GetAccountByAddressRequest) (*pb.GetAccountByAddressResponse, error)
}
