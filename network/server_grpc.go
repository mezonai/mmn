package network

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"time"

	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/store"

	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/mempool"
	pb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/service"
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
	blockStore    store.BlockStore
	mempool       *mempool.Mempool
	eventRouter   *events.EventRouter                    // Event router for complex event logic
	txTracker     interfaces.TransactionTrackerInterface // Transaction state tracker
	txSvc         interfaces.TxService
	acctSvc       interfaces.AccountService
}

func NewGRPCServer(addr string, pubKeys map[string]ed25519.PublicKey, blockDir string,
	ld *ledger.Ledger, collector *consensus.Collector,
	selfID string, priv ed25519.PrivateKey, validator *validator.Validator, blockStore store.BlockStore, mempool *mempool.Mempool, eventRouter *events.EventRouter, txTracker interfaces.TransactionTrackerInterface) *grpc.Server {

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
		txTracker:     txTracker,
	}

	// Initialize shared services
	s.txSvc = service.NewTxService(ld, mempool, blockStore, txTracker)
	s.acctSvc = service.NewAccountService(ld, mempool, txTracker)

	grpcSrv := grpc.NewServer()
	pb.RegisterBlockServiceServer(grpcSrv, s)
	pb.RegisterVoteServiceServer(grpcSrv, s)
	pb.RegisterTxServiceServer(grpcSrv, s)
	pb.RegisterAccountServiceServer(grpcSrv, s)
	pb.RegisterHealthServiceServer(grpcSrv, s)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logx.Error("GRPC SERVER", fmt.Sprintf("[gRPC] Failed to listen on %s: %v", addr, err))
		return nil
	}
	exception.SafeGo("Grpc Server", func() {
		grpcSrv.Serve(lis)
	})
	logx.Info("GRPC SERVER", "gRPC server listening on ", addr)
	return grpcSrv
}

func (s *server) AddTx(ctx context.Context, in *pb.SignedTxMsg) (*pb.AddTxResponse, error) {
	return s.txSvc.AddTx(ctx, in)
}

func (s *server) GetAccount(ctx context.Context, in *pb.GetAccountRequest) (*pb.GetAccountResponse, error) {
	return s.acctSvc.GetAccount(ctx, in)
}

func (s *server) GetCurrentNonce(ctx context.Context, in *pb.GetCurrentNonceRequest) (*pb.GetCurrentNonceResponse, error) {
	return s.acctSvc.GetCurrentNonce(ctx, in)
}

func (s *server) GetTxByHash(ctx context.Context, in *pb.GetTxByHashRequest) (*pb.GetTxByHashResponse, error) {
	return s.txSvc.GetTxByHash(ctx, in)
}

func (s *server) GetTransactionStatus(ctx context.Context, in *pb.GetTransactionStatusRequest) (*pb.TransactionStatusInfo, error) {
	return s.txSvc.GetTransactionStatus(ctx, in)
}

func (s *server) GetPendingTransactions(ctx context.Context, in *pb.GetPendingTransactionsRequest) (*pb.GetPendingTransactionsResponse, error) {
	return s.txSvc.GetPendingTransactions(ctx, in)
}

// SubscribeTransactionStatus streams transaction status updates using event-based system
func (s *server) SubscribeTransactionStatus(in *pb.SubscribeTransactionStatusRequest, stream grpc.ServerStreamingServer[pb.TransactionStatusInfo]) error {
	// Subscribe to all blockchain events
	subscriberID, eventChan := s.eventRouter.Subscribe()
	defer s.eventRouter.Unsubscribe(subscriberID)

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
			ExtraInfo:     e.Transaction().ExtraInfo,
			Amount:        utils.Uint256ToString(e.Transaction().Amount),
			TextData:      e.Transaction().TextData,
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
			ExtraInfo:     e.TxExtraInfo(),
			Amount:        utils.Uint256ToString(e.Transaction().Amount),
			TextData:      e.Transaction().TextData,
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
			ExtraInfo:     e.TxExtraInfo(),
			Amount:        utils.Uint256ToString(e.Transaction().Amount),
			TextData:      e.Transaction().TextData,
		}

	case *events.TransactionFailed:
		return &pb.TransactionStatusInfo{
			TxHash:        txHash,
			Status:        pb.TransactionStatus_FAILED,
			ErrorMessage:  e.ErrorMessage(),
			Confirmations: 0, // No confirmations for failed transactions
			Timestamp:     uint64(e.Timestamp().Unix()),
			ExtraInfo:     e.TxExtraInfo(),
			Amount:        utils.Uint256ToString(e.Transaction().Amount),
			TextData:      e.Transaction().TextData,
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

// GetBlockNumber returns current block number
func (s *server) GetBlockNumber(ctx context.Context, in *pb.EmptyParams) (*pb.GetBlockNumberResponse, error) {
	currentBlock := uint64(0)

	if s.blockStore != nil {
		currentBlock = s.blockStore.GetLatestFinalizedSlot()
	}

	return &pb.GetBlockNumberResponse{
		BlockNumber: currentBlock,
	}, nil
}

// GetBlockByNumber retrieves a block by its number
func (s *server) GetBlockByNumber(ctx context.Context, in *pb.GetBlockByNumberRequest) (*pb.GetBlockByNumberResponse, error) {
	logx.Info("GRPC SERVER", fmt.Sprintf("GetBlockByNumber: retrieving blocks %v", in.BlockNumbers))
	blocks := make([]*pb.Block, 0, len(in.BlockNumbers))

	for _, num := range in.BlockNumbers {
		block := s.blockStore.Block(num)
		if block == nil {
			return nil, status.Errorf(codes.NotFound, "block %d not found", num)
		}

		entries := make([]*pb.Entry, 0, len(block.Entries))
		var allTxHashes []string

		for _, entry := range block.Entries {
			entries = append(entries, &pb.Entry{
				NumHashes: entry.NumHashes,
				Hash:      entry.Hash[:],
				TxHashes:  entry.TxHashes,
			})
			allTxHashes = append(allTxHashes, entry.TxHashes...)
		}

		blockTxs := make([]*pb.TransactionData, 0, len(allTxHashes))
		for _, txHash := range allTxHashes {
			tx, txMeta, errTx, errTxMeta := s.ledger.GetTxByHash(txHash)

			if errTx != nil || errTxMeta != nil {
				errMsg := fmt.Errorf("error while retrieving tx by hash: %v, %v", errTx, errTxMeta)
				return nil, status.Errorf(codes.NotFound, "tx %s not found: %v", txHash, errMsg)
			}

			txStatus := utils.TxMetaStatusToProtoTxStatus(txMeta.Status)
			blockTxs = append(blockTxs, &pb.TransactionData{
				TxHash:    txHash,
				Sender:    tx.Sender,
				Recipient: tx.Recipient,
				Amount:    utils.Uint256ToString(tx.Amount),
				Nonce:     tx.Nonce,
				Timestamp: tx.Timestamp,
				Status:    txStatus,
				TextData:  tx.TextData,
				ExtraInfo: tx.ExtraInfo,
			})
		}

		pbBlock := &pb.Block{
			Slot:            block.Slot,
			PrevHash:        block.PrevHash[:],
			Entries:         entries,
			LeaderId:        block.LeaderID,
			Timestamp:       block.Timestamp,
			Hash:            block.Hash[:],
			Signature:       block.Signature,
			TransactionData: blockTxs,
		}

		blocks = append(blocks, pbBlock)
	}

	logx.Info("GRPC SERVER", fmt.Sprintf("GetBlockByNumber: retrieved blocks %v", in.BlockNumbers))
	return &pb.GetBlockByNumberResponse{
		Blocks:   blocks,
		Decimals: uint32(config.GetDecimalsFactor()),
	}, nil
}

// GetAccountByAddress is a convenience RPC under AccountService to fetch account info
func (s *server) GetAccountByAddress(ctx context.Context, in *pb.GetAccountByAddressRequest) (*pb.GetAccountByAddressResponse, error) {
	return s.acctSvc.GetAccountByAddress(ctx, in)
}
