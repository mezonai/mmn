package service

import (
	"context"
	"crypto/ed25519"
	"fmt"

	"github.com/mezonai/mmn/common"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	pb "github.com/mezonai/mmn/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BlServiceImpl provides blacklist management backed by mempool storage.
type BlServiceImpl struct {
	mempool *mempool.Mempool
	selfID  string
}

func NewBlService(mp *mempool.Mempool, selfID string) *BlServiceImpl {
	return &BlServiceImpl{mempool: mp, selfID: selfID}
}

func (s *BlServiceImpl) Add(ctx context.Context, in *pb.SignedBL) (*pb.BlacklistResponse, error) {
	if s.mempool == nil {
		return &pb.BlacklistResponse{Ok: false}, status.Errorf(codes.Unavailable, "mempool not available")
	}
	if in == nil || in.Address == "" {
		return &pb.BlacklistResponse{Ok: false}, status.Errorf(codes.InvalidArgument, "address is required")
	}
	if ok := s.verifyBlacklistSignature(in); !ok {
		return &pb.BlacklistResponse{Ok: false}, status.Errorf(codes.PermissionDenied, "invalid admin signature")
	}
	reason := in.Reason
	if reason == "" {
		reason = "manual blacklist"
	}
	if err := s.mempool.AddToBlacklist(in.Address, reason); err != nil {
		return &pb.BlacklistResponse{Ok: false}, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return &pb.BlacklistResponse{Ok: true}, nil
}

func (s *BlServiceImpl) List(ctx context.Context, in *pb.SignedBL) (*pb.ListBlacklistResponse, error) {
	if s.mempool == nil {
		return nil, status.Errorf(codes.Unavailable, "mempool not available")
	}
	if ok := s.verifyBlacklistSignature(in); !ok {
		return nil, status.Errorf(codes.PermissionDenied, "invalid admin signature")
	}

	entries := s.mempool.ListBlacklist()
	return &pb.ListBlacklistResponse{Entries: entries}, nil
}

func (s *BlServiceImpl) Remove(ctx context.Context, in *pb.SignedBL) (*pb.BlacklistResponse, error) {
	if s.mempool == nil {
		return &pb.BlacklistResponse{Ok: false}, status.Errorf(codes.Unavailable, "mempool not available")
	}
	if in == nil || in.Address == "" {
		return &pb.BlacklistResponse{Ok: false}, status.Errorf(codes.InvalidArgument, "address is required")
	}
	if ok := s.verifyBlacklistSignature(in); !ok {
		return &pb.BlacklistResponse{Ok: false}, status.Errorf(codes.PermissionDenied, "invalid admin signature")
	}
	if err := s.mempool.RemoveFromBlacklist(in.Address); err != nil {
		return &pb.BlacklistResponse{Ok: false}, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return &pb.BlacklistResponse{Ok: true}, nil
}

func (s *BlServiceImpl) verifyBlacklistSignature(in *pb.SignedBL) bool {

	if in == nil || in.AdminAddress == "" {
		logx.Debug("Verify Blacklist Signature", "missing admin address")
		return false
	}

	logx.Debug("Verify Blacklist Signature", in.AdminAddress, s.selfID)
	if in.AdminAddress != s.selfID {
		logx.Debug("Verify Blacklist Signature", "admin address does not match selfID")
		return false
	}
	pubBytes, err := common.DecodeBase58ToBytes(in.AdminAddress)
	if err != nil {
		logx.Debug("Verify Blacklist Signature", "failed to decode admin address")
		return false
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		logx.Debug("Verify Blacklist Signature", "invalid public key size")
		return false
	}
	msg := fmt.Sprintf("%s|%s", in.AdminAddress, in.Address)
	return ed25519.Verify(ed25519.PublicKey(pubBytes), []byte(msg), in.Sig)
}
