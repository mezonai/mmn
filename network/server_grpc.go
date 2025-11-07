package network

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/errors"
	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	pb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/security/ratelimit"
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/types"
	"github.com/mezonai/mmn/utils"
	"github.com/mezonai/mmn/validator"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type server struct {
	pb.UnimplementedBlockServiceServer
	pb.UnimplementedTxServiceServer
	pb.UnimplementedAccountServiceServer
	pb.UnimplementedHealthServiceServer
	ledger      *ledger.Ledger
	selfID      string
	validator   *validator.Validator
	blockStore  store.BlockStore
	mempool     *mempool.Mempool
	eventRouter *events.EventRouter          // Event router for complex event logic
	rateLimiter *ratelimit.GlobalRateLimiter // Rate limiter for transaction submission protection
	txSvc       interfaces.TxService
	acctSvc     interfaces.AccountService
	healthSvc   interfaces.HealthService
}

func NewGRPCServer(addr string, ld *ledger.Ledger, selfID string, val *validator.Validator, blockStore store.BlockStore, mp *mempool.Mempool, eventRouter *events.EventRouter, rateLimiter *ratelimit.GlobalRateLimiter, enableRateLimit bool, txSvc interfaces.TxService, acctSvc interfaces.AccountService, healthSvc interfaces.HealthService) *grpc.Server {
	s := &server{
		ledger:      ld,
		selfID:      selfID,
		blockStore:  blockStore,
		validator:   val,
		mempool:     mp,
		eventRouter: eventRouter,
		rateLimiter: rateLimiter,
		txSvc:       txSvc,
		acctSvc:     acctSvc,
		healthSvc:   healthSvc,
	}

	// Initialize shared services
	unaryInterceptors := []grpc.UnaryServerInterceptor{
		defaultDeadlineUnaryInterceptor(GRPCDefaultDeadline),
	}
	streamInterceptors := []grpc.StreamServerInterceptor{}

	if enableRateLimit {
		unaryInterceptors = append([]grpc.UnaryServerInterceptor{
			securityUnaryInterceptor(s.rateLimiter),
		}, unaryInterceptors...)

		streamInterceptors = append([]grpc.StreamServerInterceptor{
			securityStreamInterceptor(s.rateLimiter),
		}, streamInterceptors...)
	}

	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(unaryInterceptors...),
		grpc.ChainStreamInterceptor(streamInterceptors...),
		grpc.MaxRecvMsgSize(GRPCMaxRecvMsgSize),
		grpc.MaxSendMsgSize(GRPCMaxSendMsgSize),
	)
	pb.RegisterBlockServiceServer(grpcSrv, s)
	pb.RegisterTxServiceServer(grpcSrv, s)
	pb.RegisterAccountServiceServer(grpcSrv, s)
	pb.RegisterHealthServiceServer(grpcSrv, s)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logx.Error("GRPC SERVER", fmt.Sprintf("[gRPC] Failed to listen on %s: %v", addr, err))
		return nil
	}
	exception.SafeGoWithPanic("Grpc Server", func() {
		err = grpcSrv.Serve(lis)
		if err != nil {
			logx.Error("GRPC SERVER", fmt.Sprintf("Failed to serve gRPC server: %v", err))
			panic(err)
		}
	})
	logx.Info("GRPC SERVER", "gRPC server listening on ", addr)
	return grpcSrv
}

func securityUnaryInterceptor(rateLimiter *ratelimit.GlobalRateLimiter) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		clientIP := extractClientIP(ctx)
		logx.Debug("SECURITY", "Client IP:", clientIP, "Method:", info.FullMethod)
		if rateLimiter != nil {
			if !rateLimiter.IsIPAllowed(clientIP) {
				logx.Warn("SECURITY", "Alert spam from IP:", clientIP, "Method:", info.FullMethod)
				return nil, status.Errorf(codes.ResourceExhausted, "Too many requests")
			}
			exception.SafeGo("TrackIPRequest", func() {
				rateLimiter.TrackIPRequest(clientIP)
			})
		}

		return handler(ctx, req)
	}
}

func securityStreamInterceptor(rateLimiter *ratelimit.GlobalRateLimiter) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		clientIP := extractClientIP(ss.Context())
		logx.Debug("SECURITY", "Client IP:", clientIP, "Method:", info.FullMethod)
		if rateLimiter != nil {
			if !rateLimiter.IsIPAllowed(clientIP) {
				logx.Warn("SECURITY", "IP limited (stream):", clientIP, "Method:", info.FullMethod)
				return status.Errorf(codes.ResourceExhausted, "ip rate limit")
			}
			exception.SafeGo("TrackIPRequest", func() {
				rateLimiter.TrackIPRequest(clientIP)
			})
		}
		return handler(srv, ss)
	}
}

func defaultDeadlineUnaryInterceptor(defaultTimeout time.Duration) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) <= 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
			defer cancel()
		}
		return handler(ctx, req)
	}
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
			statusUpdate := s.convertEventToStatusUpdate(event, event.Transaction().Hash())
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

	case *events.HeartBeatEvent:
		return &pb.TransactionStatusInfo{
			TxHash:        events.HeartBeat,
			Status:        pb.TransactionStatus_PENDING,
			Confirmations: 0, // No confirmations for mempool transactions
			Timestamp:     uint64(e.Timestamp().Unix()),
			ExtraInfo:     "",
			Amount:        "0",
			TextData:      events.HeartBeat,
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
	resp, err := s.healthSvc.Check(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to perform health check: %v", err)
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
	logx.Info("GRPC SERVER", fmt.Sprintf("GetBlockByNumber: retrieving blocks %d", len(in.BlockNumbers)))

	if len(in.BlockNumbers) == 0 {
		return &pb.GetBlockByNumberResponse{
			Blocks:   []*pb.Block{},
			Decimals: uint32(config.GetDecimalsFactor()),
		}, nil
	}

	// Use batch operation to get all blocks - single CGO call!
	blockMap, err := s.blockStore.GetBatch(in.BlockNumbers)
	if err != nil {
		logx.Error("GRPC SERVER", fmt.Sprintf("failed to batch get blocks: %v", err))
		return nil, status.Errorf(codes.Internal, "failed to batch get blocks: %v", err)
	}

	// Collect ALL transaction hashes from ALL blocks first
	var allTxHashes []string
	blockTxMap := make(map[uint64][]string) // Map slot to its tx hashes

	for _, slot := range in.BlockNumbers {
		block, exists := blockMap[slot]
		if !exists {
			logx.Error("GRPC SERVER", fmt.Sprintf("block %d not found", slot))
			return nil, status.Errorf(codes.NotFound, "block %d not found", slot)
		}

		var blockTxHashes []string
		for _, entry := range block.Entries {
			blockTxHashes = append(blockTxHashes, entry.TxHashes...)
			allTxHashes = append(allTxHashes, entry.TxHashes...)
		}
		blockTxMap[slot] = blockTxHashes
	}

	// Batch get ALL transactions and metadata - only 2 CGO calls instead of 2*N!
	var txs []*transaction.Transaction
	var txMetaMap map[string]*types.TransactionMeta
	if len(allTxHashes) > 0 {
		var errTx error
		txs, txMetaMap, errTx = s.ledger.GetTxBatch(allTxHashes)
		if errTx != nil {
			logx.Error("GRPC SERVER", fmt.Sprintf("failed to batch get transactions: %v", errTx))
			return nil, status.Errorf(codes.Internal, "failed to batch get transactions: %v", errTx)
		}
	}

	// Create a map for quick transaction lookup
	txMap := make(map[string]*transaction.Transaction)
	for _, tx := range txs {
		txMap[tx.Hash()] = tx
	}

	// Build response blocks in the same order as requested
	blocks := make([]*pb.Block, 0, len(in.BlockNumbers))
	for _, slot := range in.BlockNumbers {
		block := blockMap[slot]
		blockTxHashes := blockTxMap[slot]

		// Build transaction data for this block
		blockTxs := make([]*pb.TransactionData, 0, len(blockTxHashes))
		for _, txHash := range blockTxHashes {
			tx, txExists := txMap[txHash]
			txMeta, metaExists := txMetaMap[txHash]

			if !txExists || !metaExists {
				logx.Error("GRPC SERVER", fmt.Sprintf("tx %s not found in batch result", txHash))
				return nil, errors.NewError(errors.ErrCodeTransactionNotFound, errors.ErrMsgTransactionNotFound)
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
			Entries:         []*pb.Entry{}, // Empty as requested - indexer doesn't use this
			LeaderId:        block.LeaderID,
			Timestamp:       block.Timestamp,
			Hash:            block.Hash[:],
			Signature:       block.Signature,
			TransactionData: blockTxs,
		}

		blocks = append(blocks, pbBlock)
	}

	logx.Info("GRPC SERVER", fmt.Sprintf("GetBlockByNumber: retrieved %d blocks", len(blocks)))
	return &pb.GetBlockByNumberResponse{
		Blocks:   blocks,
		Decimals: uint32(config.GetDecimalsFactor()),
	}, nil
}

// GetBlockByRange retrieves blocks in a range with optimized response structure
func (s *server) GetBlockByRange(ctx context.Context, in *pb.GetBlockByRangeRequest) (*pb.GetBlockByRangeResponse, error) {
	logx.Info("GRPC SERVER", fmt.Sprintf("GetBlockByRange: retrieving blocks from %d to %d", in.FromSlot, in.ToSlot))

	// Validate input
	if in.FromSlot > in.ToSlot {
		logx.Error("GRPC SERVER", fmt.Sprintf("from_slot (%d) cannot be greater than to_slot (%d)", in.FromSlot, in.ToSlot))
		return nil, status.Errorf(codes.InvalidArgument, "from_slot (%d) cannot be greater than to_slot (%d)", in.FromSlot, in.ToSlot)
	}

	// Calculate total blocks in range
	totalBlocks := in.ToSlot - in.FromSlot + 1

	// Reject if range is too large
	const maxRange = 500
	if totalBlocks > maxRange {
		logx.Error("GRPC SERVER", fmt.Sprintf("range too large: %d blocks requested, maximum allowed: %d", totalBlocks, maxRange))
		return nil, status.Errorf(codes.InvalidArgument, "range too large: %d blocks requested, maximum allowed: %d", totalBlocks, maxRange)
	}

	// Prepare slot range for batch operation
	slots := make([]uint64, 0, totalBlocks)
	for slot := in.FromSlot; slot <= in.ToSlot; slot++ {
		slots = append(slots, slot)
	}

	// Use batch operation to get all blocks - single CGO call!
	blockMap, err := s.blockStore.GetBatch(slots)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to batch get blocks: %v", err)
	}

	// Collect ALL transaction hashes from ALL blocks first
	var allTxHashes []string
	blockTxMap := make(map[uint64][]string) // Map slot to its tx hashes
	errs := make([]string, 0)

	for _, slot := range slots {
		block, exists := blockMap[slot]
		if !exists {
			logx.Error("GRPC SERVER", fmt.Sprintf("Block %d not found, skipping", slot))
			errs = append(errs, fmt.Sprintf("Block %d not found, skipping", slot))
			continue
		}

		var blockTxHashes []string
		for _, entry := range block.Entries {
			blockTxHashes = append(blockTxHashes, entry.TxHashes...)
			allTxHashes = append(allTxHashes, entry.TxHashes...)
		}
		blockTxMap[slot] = blockTxHashes
	}

	// Single batch call for ALL transactions across ALL blocks!
	txs, txMetas, err := s.ledger.GetTxBatch(allTxHashes)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to batch get all transactions: %v", err)
	}

	// Create tx lookup maps for fast access
	txMap := make(map[string]*transaction.Transaction, len(txs))
	for _, tx := range txs {
		txMap[tx.Hash()] = tx
	}

	blocks := make([]*pb.BlockInfo, 0, len(blockMap))

	// Process blocks in order
	for _, slot := range slots {
		block, exists := blockMap[slot]
		if !exists {
			continue // Already logged warning above
		}

		blockTxHashes := blockTxMap[slot]
		blockTxs := make([]*pb.TransactionData, 0, len(blockTxHashes))

		for _, txHash := range blockTxHashes {
			tx, txExists := txMap[txHash]
			txMeta, metaExists := txMetas[txHash]

			if !txExists || !metaExists {
				logx.Error("GRPC SERVER", fmt.Sprintf("Transaction or meta not found for tx %s in block %d", txHash, slot))
				errs = append(errs, fmt.Sprintf("Transaction or meta not found for tx %s in block %d", txHash, slot))
				continue
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

		// Create optimized block (without entries duplication)
		optimizedBlock := &pb.BlockInfo{
			Slot:            block.Slot,
			PrevHash:        block.PrevHash[:],
			LeaderId:        block.LeaderID,
			Timestamp:       block.Timestamp,
			Hash:            block.Hash[:],
			Signature:       block.Signature,
			TransactionData: blockTxs,
		}

		blocks = append(blocks, optimizedBlock)
	}

	logx.Info("GRPC SERVER", fmt.Sprintf("GetBlockByRange: retrieved %d blocks (requested range: %d)",
		len(blocks), totalBlocks))

	return &pb.GetBlockByRangeResponse{
		Blocks:      blocks,
		TotalBlocks: uint32(len(blocks)),
		Decimals:    uint32(config.GetDecimalsFactor()),
		Errors:      errs,
	}, nil
}
