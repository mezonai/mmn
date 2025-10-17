package interfaces

import (
	"context"

	pb "github.com/mezonai/mmn/proto"
)

type BlService interface {
	Add(ctx context.Context, in *pb.SignedBL) (*pb.BlacklistResponse, error)
	List(ctx context.Context, in *pb.SignedBL) (*pb.ListBlacklistResponse, error)
	Remove(ctx context.Context, in *pb.SignedBL) (*pb.BlacklistResponse, error)
}
