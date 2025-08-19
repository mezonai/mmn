package network

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"time"

	"github.com/mezonai/mmn/blockstore"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/mempool"
	pb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/utils"
	"github.com/mezonai/mmn/validator"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type server struct {
	pb.UnimplementedBlockServiceServer
	pb.UnimplementedVoteServiceServer
	pb.UnimplementedTxServiceServer
	pb.UnimplementedAccountServiceServer
	pb.UnimplementedHealthServiceServer
	pubKeys       map[string]ed25519.PublicKey
	blockDir      string
	ledger        *ledger.Ledger
	voteCollector *consensus.Collector
	selfID        string
	privKey       ed25519.PrivateKey
	validator     *validator.Validator
	blockStore    blockstore.Store
	mempool       *mempool.Mempool
	eventRouter   *events.EventRouter // Event router for complex event logic
}

func NewGRPCServer(addr string, pubKeys map[string]ed25519.PublicKey, blockDir string,
	ld *ledger.Ledger, collector *consensus.Collector,
	selfID string, priv ed25519.PrivateKey, validator *validator.Validator, blockStore blockstore.Store, mempool *mempool.Mempool, eventRouter *events.EventRouter) *grpc.Server {

	s := &server{
		pubKeys:       pubKeys,
		blockDir:      blockDir,
		ledger:        ld,
		voteCollector: collector,
		selfID:        selfID,
		privKey:       priv,
		blockStore:    blockStore,
		validator:     validator,
		mempool:       mempool,
		eventRouter:   eventRouter,
	}

	grpcSrv := grpc.NewServer()
	pb.RegisterBlockServiceServer(grpcSrv, s)
	pb.RegisterVoteServiceServer(grpcSrv, s)
	pb.RegisterTxServiceServer(grpcSrv, s)
	pb.RegisterAccountServiceServer(grpcSrv, s)
	pb.RegisterHealthServiceServer(grpcSrv, s)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("[gRPC] Failed to listen on %s: %v\n", addr, err)
		return nil
	}
	exception.SafeGo("Grpc Server", func() {
		grpcSrv.Serve(lis)
	})
	fmt.Printf("[gRPC] server listening on %s\n", addr)
	return grpcSrv
}

func (s *server) AddTx(ctx context.Context, in *pb.SignedTxMsg) (*pb.AddTxResponse, error) {
	fmt.Printf("[gRPC] received tx %+v\n", in.TxMsg)
	tx, err := utils.FromProtoSignedTx(in)
	if err != nil {
		fmt.Printf("[gRPC] FromProtoSignedTx error: %v\n", err)
		return &pb.AddTxResponse{Ok: false, Error: "invalid tx"}, nil
	}

	// Add validation checks before adding to mempool
	// 1. Verify signature
	if !tx.Verify() {
		fmt.Printf("[gRPC] Signature verification failed for tx: %+v\n", tx)
		return &pb.AddTxResponse{Ok: false, Error: "invalid signature"}, nil
	}

	// 2. Check for zero amount
	if tx.Amount == 0 {
		fmt.Printf("[gRPC] Zero amount detected: %d\n", tx.Amount)
		return &pb.AddTxResponse{Ok: false, Error: "zero amount not allowed"}, nil
	}

	// 3. Check sender account exists (except for faucet transactions)
	senderAccount := s.ledger.GetAccount(tx.Sender)
	if senderAccount == nil {
		fmt.Printf("[gRPC] Sender account %s does not exist\n", tx.Sender)
		return &pb.AddTxResponse{Ok: false, Error: fmt.Sprintf("sender account %s does not exist", tx.Sender)}, nil
	}

	// 4. Check nonce is exactly next expected value
	if tx.Nonce != senderAccount.Nonce+1 {
		fmt.Printf("[gRPC] Invalid nonce: expected %d, got %d\n", senderAccount.Nonce+1, tx.Nonce)
		return &pb.AddTxResponse{Ok: false, Error: "invalid nonce"}, nil
	}

	// 5. Check sufficient balance
	if senderAccount.Balance < tx.Amount {
		fmt.Printf("[gRPC] Insufficient balance: balance=%d, amount=%d\n", senderAccount.Balance, tx.Amount)
		return &pb.AddTxResponse{Ok: false, Error: "insufficient balance"}, nil
	}

	txHash, ok := s.mempool.AddTx(tx, true)
	if !ok {
		return &pb.AddTxResponse{Ok: false, Error: "mempool full"}, nil
	}
	return &pb.AddTxResponse{Ok: true, TxHash: txHash}, nil
}

func (s *server) GetAccount(ctx context.Context, in *pb.GetAccountRequest) (*pb.GetAccountResponse, error) {
	addr := in.Address
	acc := s.ledger.GetAccount(addr)
	if acc == nil {
		return &pb.GetAccountResponse{
			Address: addr,
			Balance: 0,
			Nonce:   0,
		}, nil
	}
	return &pb.GetAccountResponse{
		Address: addr,
		Balance: acc.Balance,
		Nonce:   acc.Nonce,
	}, nil
}

func (s *server) GetTxHistory(ctx context.Context, in *pb.GetTxHistoryRequest) (*pb.GetTxHistoryResponse, error) {
	addr := in.Address
	total, txs := s.ledger.GetTxs(addr, in.Limit, in.Offset, in.Filter)
	txMetas := make([]*pb.TxMeta, len(txs))
	for i, tx := range txs {
		txMetas[i] = &pb.TxMeta{
			Sender:    tx.Sender,
			Recipient: tx.Recipient,
			Amount:    tx.Amount,
			Nonce:     tx.Nonce,
			Timestamp: tx.Timestamp,
			Status:    pb.TxMeta_CONFIRMED,
		}
	}
	return &pb.GetTxHistoryResponse{
		Total: total,
		Txs:   txMetas,
	}, nil
}

// GetTransactionStatus returns real-time status by checking mempool and blockstore.
func (s *server) GetTransactionStatus(ctx context.Context, in *pb.GetTransactionStatusRequest) (*pb.TransactionStatusInfo, error) {
	txHash := in.TxHash

	// 1) Check mempool
	if s.mempool != nil {
		data, ok := s.mempool.GetTransaction(txHash)
		if ok {
			// Parse tx to compute client-hash
			tx, err := utils.ParseTx(data)
			if err == nil {
				if tx.Hash() == txHash {
					return &pb.TransactionStatusInfo{
						TxHash:        txHash,
						Status:        pb.TransactionStatus_PENDING,
						Confirmations: 0, // No confirmations for mempool transactions
						Timestamp:     uint64(time.Now().Unix()),
					}, nil
				}
			}
		}
	}

	// 2) Search in stored blocks
	if s.blockStore != nil {
		slot, blk, _, found := s.blockStore.GetTransactionBlockInfo(txHash)
		if found {
			confirmations := s.blockStore.GetConfirmations(slot)
			status := pb.TransactionStatus_CONFIRMED
			if confirmations > 1 {
				status = pb.TransactionStatus_FINALIZED
			}

			return &pb.TransactionStatusInfo{
				TxHash:        txHash,
				Status:        status,
				BlockSlot:     slot,
				BlockHash:     blk.HashString(),
				Confirmations: confirmations,
				Timestamp:     uint64(time.Now().Unix()),
			}, nil
		}
	}

	// 3) Transaction not found anywhere -> return nil and error
	return nil, fmt.Errorf("transaction not found: %s", txHash)
}

// SubscribeTransactionStatus streams transaction status updates using event-based system
func (s *server) SubscribeTransactionStatus(in *pb.SubscribeTransactionStatusRequest, stream grpc.ServerStreamingServer[pb.TransactionStatusInfo]) error {
	// Subscribe to all blockchain events
	eventChan := s.eventRouter.Subscribe()
	defer s.eventRouter.Unsubscribe(eventChan)

	// Wait for events indefinitely (client keeps connection open)
	for {
		select {
		case event := <-eventChan:
			// Convert event to status update for the specific transaction
			statusUpdate := s.convertEventToStatusUpdate(event, event.TxHash())
			if statusUpdate != nil {
				if err := stream.Send(statusUpdate); err != nil {
					return err
				}
			}

		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// convertEventToStatusUpdate converts blockchain events to transaction status updates
func (s *server) convertEventToStatusUpdate(event events.BlockchainEvent, txHash string) *pb.TransactionStatusInfo {
	switch e := event.(type) {
	case *events.TransactionAddedToMempool:
		return &pb.TransactionStatusInfo{
			TxHash:        txHash,
			Status:        pb.TransactionStatus_PENDING,
			Confirmations: 0, // No confirmations for mempool transactions
			Timestamp:     uint64(e.Timestamp().Unix()),
		}

	case *events.TransactionIncludedInBlock:
		// Transaction included in block = CONFIRMED status
		confirmations := s.blockStore.GetConfirmations(e.BlockSlot())

		return &pb.TransactionStatusInfo{
			TxHash:        txHash,
			Status:        pb.TransactionStatus_CONFIRMED,
			BlockSlot:     e.BlockSlot(),
			BlockHash:     e.BlockHash(),
			Confirmations: confirmations,
			Timestamp:     uint64(e.Timestamp().Unix()),
		}

	case *events.TransactionFinalized:
		// Transaction finalized = FINALIZED status
		confirmations := s.blockStore.GetConfirmations(e.BlockSlot())

		return &pb.TransactionStatusInfo{
			TxHash:        txHash,
			Status:        pb.TransactionStatus_FINALIZED,
			BlockSlot:     e.BlockSlot(),
			BlockHash:     e.BlockHash(),
			Confirmations: confirmations,
			Timestamp:     uint64(e.Timestamp().Unix()),
		}

	case *events.TransactionFailed:
		return &pb.TransactionStatusInfo{
			TxHash:        txHash,
			Status:        pb.TransactionStatus_FAILED,
			ErrorMessage:  e.ErrorMessage(),
			Confirmations: 0, // No confirmations for failed transactions
			Timestamp:     uint64(e.Timestamp().Unix()),
		}
	}

	return nil
}

// Health check methods
func (s *server) Check(ctx context.Context, in *pb.Empty) (*pb.HealthCheckResponse, error) {
	return s.performHealthCheck(ctx)
}

func (s *server) Watch(in *pb.Empty, stream pb.HealthService_WatchServer) error {
	ticker := time.NewTicker(5 * time.Second) // Send health status every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-ticker.C:
			resp, err := s.performHealthCheck(stream.Context())
			if err != nil {
				return err
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	}
}

// performHealthCheck performs the actual health check logic
func (s *server) performHealthCheck(ctx context.Context) (*pb.HealthCheckResponse, error) {
	// Check if context is cancelled
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

	if s.validator != nil && s.validator.Recorder != nil {
		currentSlot = s.validator.Recorder.CurrentSlot()
	}

	if s.blockStore != nil {
		// Get the latest finalized block height
		// For now, we'll use a simple approach to get block height
		// In a real implementation, you might want to track this separately
		blockHeight = currentSlot // Use current slot as approximation
	}

	// Get mempool size
	mempoolSize := uint64(0)
	if s.mempool != nil {
		mempoolSize = uint64(s.mempool.Size())
	}

	// Determine if node is leader or follower
	isLeader := false
	isFollower := false
	if s.validator != nil {
		isLeader = s.validator.IsLeader(currentSlot)
		isFollower = s.validator.IsFollower(currentSlot)
	}

	// Check if core services are healthy
	status := pb.HealthCheckResponse_SERVING

	// Basic health checks
	if s.ledger == nil {
		status = pb.HealthCheckResponse_NOT_SERVING
	}
	if s.blockStore == nil {
		status = pb.HealthCheckResponse_NOT_SERVING
	}
	if s.mempool == nil {
		status = pb.HealthCheckResponse_NOT_SERVING
	}

	// Create response
	resp := &pb.HealthCheckResponse{
		Status:       status,
		NodeId:       s.selfID,
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
	if status == pb.HealthCheckResponse_NOT_SERVING {
		resp.ErrorMessage = "One or more core services are not available"
	}

	return resp, nil
}
