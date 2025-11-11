package service

import (
	"context"
	"time"

	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/validator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/mezonai/mmn/proto"
)

type HealthServiceImpl struct {
	ledger     *ledger.Ledger
	blockStore store.BlockStore
	mempool    *mempool.Mempool
	validator  *validator.Validator
	selfID     string
}

func NewHealthService(ld *ledger.Ledger, bs store.BlockStore, mp *mempool.Mempool, val *validator.Validator, selfID string) *HealthServiceImpl {
	return &HealthServiceImpl{ledger: ld, blockStore: bs, mempool: mp, validator: val, selfID: selfID}
}

func (hs *HealthServiceImpl) Check(ctx context.Context) (*pb.HealthCheckResponse, error) {
	// Check if context is canceled
	select {
	case <-ctx.Done():
		return nil, status.Errorf(codes.DeadlineExceeded, "health check timeout")
	default:
	}

	// Get current node status
	now := time.Now()

	// Calculate uptime (assuming server started at some point)
	// In a real implementation, you'd track server start time
	uptime := uint64(now.Unix()) // Placeholder for actual uptime

	// Get current slot and block height
	currentSlot := uint64(0)
	blockHeight := uint64(0)

	if hs.validator != nil && hs.validator.Recorder != nil {
		currentSlot = hs.validator.Recorder.CurrentSlot()
	}

	if hs.blockStore != nil {
		// Get the latest finalized block height
		// For now, we'll use a simple approach to get block height
		// In a real implementation, you might want to track this separately
		blockHeight = currentSlot // Use current slot as approximation
	}

	// Get mempool size
	mempoolSize := uint64(0)
	if hs.mempool != nil {
		mempoolSize = uint64(hs.mempool.Size())
	}

	// Determine if node is leader or follower
	isLeader := false
	isFollower := false
	if hs.validator != nil {
		isLeader = hs.validator.IsLeader(currentSlot)
		isFollower = hs.validator.IsFollower(currentSlot)
	}

	// Check if core services are healthy
	hcStatus := pb.HealthCheckResponse_SERVING

	// Basic health checks
	if hs.ledger == nil {
		hcStatus = pb.HealthCheckResponse_NOT_SERVING
	}
	if hs.blockStore == nil {
		hcStatus = pb.HealthCheckResponse_NOT_SERVING
	}
	if hs.mempool == nil {
		hcStatus = pb.HealthCheckResponse_NOT_SERVING
	}

	// Create response
	resp := &pb.HealthCheckResponse{
		Status:       hcStatus,
		NodeId:       hs.selfID,
		Timestamp:    uint64(now.Unix()),
		CurrentSlot:  currentSlot,
		BlockHeight:  blockHeight,
		MempoolSize:  mempoolSize,
		IsLeader:     isLeader,
		IsFollower:   isFollower,
		Version:      "1.0.0", // You can make this configurable
		Uptime:       uptime,
		ErrorMessage: "",
	}

	// If there are any errors, set status accordingly
	if hcStatus == pb.HealthCheckResponse_NOT_SERVING {
		resp.ErrorMessage = "One or more core services are not available"
	}

	return resp, nil
}
