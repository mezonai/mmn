package network

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"time"

	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/ratelimit"
	"github.com/mezonai/mmn/store"

	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/mempool"
	pb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/utils"
	"github.com/mezonai/mmn/validator"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
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
	rateLimiter   *ratelimit.GlobalRateLimiter           // Rate limiter for transaction submission protection
}

func NewGRPCServer(addr string, pubKeys map[string]ed25519.PublicKey, blockDir string,
	ld *ledger.Ledger, collector *consensus.Collector,
	selfID string, priv ed25519.PrivateKey, validator *validator.Validator, blockStore store.BlockStore, mempool *mempool.Mempool, eventRouter *events.EventRouter, txTracker interfaces.TransactionTrackerInterface, rateLimiter *ratelimit.GlobalRateLimiter) *grpc.Server {

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
		rateLimiter:   rateLimiter,
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
	logx.Info("GRPC", fmt.Sprintf("received tx %+v", in.TxMsg))

	// Extract client IP for rate limiting
	clientIP := "unknown"
	if p, ok := peer.FromContext(ctx); ok {
		if addr, ok := p.Addr.(*net.TCPAddr); ok {
			clientIP = addr.IP.String()
		}
	}

	// Validate input before parsing to prevent serialization errors
	if in.TxMsg == nil {
		return &pb.AddTxResponse{Ok: false, Error: "missing transaction data"}, nil
	}

	// Parse transaction to get sender for wallet-based rate limiting
	tx, err := utils.FromProtoSignedTx(in)
	if err != nil {
		logx.Error("GRPC", fmt.Sprintf("FromProtoSignedTx error from IP %s: %v", clientIP, err))
		return &pb.AddTxResponse{Ok: false, Error: "invalid transaction format"}, nil
	}

	// Apply rate limiting if rate limiter is configured
	if s.rateLimiter != nil {
		if !s.rateLimiter.AllowAll(clientIP, tx.Sender) {
			logx.Warn("GRPC", fmt.Sprintf("Rate limit exceeded for IP: %s, Wallet: %s", clientIP, tx.Sender))
			return &pb.AddTxResponse{Ok: false, Error: "rate limit exceeded"}, nil
		}
	}

	// Generate server-side timestamp for security
	// Todo: remove from input and update client
	tx.Timestamp = uint64(time.Now().UnixNano() / int64(time.Millisecond))

	txHash, err := s.mempool.AddTx(tx, true)
	if err != nil {
		return &pb.AddTxResponse{Ok: false, Error: err.Error()}, nil
	}
	return &pb.AddTxResponse{Ok: true, TxHash: txHash}, nil
}

func (s *server) GetAccount(ctx context.Context, in *pb.GetAccountRequest) (*pb.GetAccountResponse, error) {
	addr := in.Address
	acc, err := s.ledger.GetAccount(addr)
	if err != nil {
		return nil, fmt.Errorf("error while retriving account: %s", err.Error())
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

func (s *server) GetCurrentNonce(ctx context.Context, in *pb.GetCurrentNonceRequest) (*pb.GetCurrentNonceResponse, error) {
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
		// For "pending", return the largest nonce among pending transactions, processing transactions, or current ledger nonce
		ledgerNonce := acc.Nonce
		largestPendingNonce := s.mempool.GetLargestPendingNonce(addr)

		var largestProcessingNonce uint64
		if s.txTracker != nil {
			largestProcessingNonce = s.txTracker.GetLargestProcessingNonce(addr)
		}

		// Find the maximum nonce across all sources
		currentNonce = ledgerNonce
		if largestPendingNonce > currentNonce {
			currentNonce = largestPendingNonce
		}
		if largestProcessingNonce > currentNonce {
			currentNonce = largestProcessingNonce
		}

		logx.Info("GRPC", fmt.Sprintf("Pending current nonce for %s: ledger: %d, mempool: %d, processing: %d, final: %d",
			addr, ledgerNonce, largestPendingNonce, largestProcessingNonce, currentNonce))
	}

	return &pb.GetCurrentNonceResponse{
		Address: addr,
		Nonce:   currentNonce,
		Tag:     tag,
	}, nil
}

func (s *server) GetTxByHash(ctx context.Context, in *pb.GetTxByHashRequest) (*pb.GetTxByHashResponse, error) {
	tx, txMeta, err := s.ledger.GetTxByHash(in.TxHash)
	if err != nil {
		return &pb.GetTxByHashResponse{Error: err.Error()}, nil
	}
	amount := utils.Uint256ToString(tx.Amount)

	txInfo := &pb.TxInfo{
		Sender:    tx.Sender,
		Recipient: tx.Recipient,
		Amount:    amount,
		Timestamp: tx.Timestamp,
		TextData:  tx.TextData,
		Nonce:     tx.Nonce,
		Slot:      txMeta.Slot,
		Blockhash: txMeta.BlockHash,
		Status:    txMeta.Status,
		ErrMsg:    txMeta.Error,
		ExtraInfo: tx.ExtraInfo,
	}
	return &pb.GetTxByHashResponse{
		Tx:       txInfo,
		Decimals: uint32(config.GetDecimalsFactor()),
	}, nil
}

func (s *server) GetTxHistory(ctx context.Context, in *pb.GetTxHistoryRequest) (*pb.GetTxHistoryResponse, error) {
	addr := in.Address
	total, txs := s.ledger.GetTxs(addr, in.Limit, in.Offset, in.Filter)
	txMetas := make([]*pb.TxMeta, len(txs))
	for i, tx := range txs {
		amount := utils.Uint256ToString(tx.Amount)

		txMetas[i] = &pb.TxMeta{
			Sender:    tx.Sender,
			Recipient: tx.Recipient,
			Amount:    amount,
			Nonce:     tx.Nonce,
			Timestamp: tx.Timestamp,
			Status:    pb.TxMeta_CONFIRMED,
			ExtraInfo: tx.ExtraInfo,
		}
	}
	return &pb.GetTxHistoryResponse{
		Total:    total,
		Txs:      txMetas,
		Decimals: uint32(config.GetDecimalsFactor()),
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
						ExtraInfo:     tx.ExtraInfo,
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
			tx, _, err := s.ledger.GetTxByHash(txHash)
			if err != nil {
				return nil, err
			}

			return &pb.TransactionStatusInfo{
				TxHash:        txHash,
				Status:        status,
				BlockSlot:     slot,
				BlockHash:     blk.HashString(),
				Confirmations: confirmations,
				Timestamp:     uint64(time.Now().Unix()),
				ExtraInfo:     tx.ExtraInfo,
			}, nil
		}
	}

	// 3) Transaction not found anywhere -> return nil and error
	return nil, fmt.Errorf("transaction not found: %s", txHash)
}

// GetPendingTransactions returns all pending transactions from mempool with total count
// Uses GetOrderedTransactions to get all transactions in FIFO order
func (s *server) GetPendingTransactions(ctx context.Context, in *pb.GetPendingTransactionsRequest) (*pb.GetPendingTransactionsResponse, error) {
	if s.mempool == nil {
		return &pb.GetPendingTransactionsResponse{
			TotalCount: 0,
			PendingTxs: []*pb.TransactionData{},
			Error:      "mempool not available",
		}, nil
	}

	// Get all ordered transaction hashes from mempool
	orderedTxHashes := s.mempool.GetOrderedTransactions()
	totalCount := uint64(len(orderedTxHashes))

	// Convert transaction hashes to detailed transaction info
	var pendingTxs []*pb.TransactionData
	for _, txHash := range orderedTxHashes {
		// Get transaction data from mempool
		txData, exists := s.mempool.GetTransaction(txHash)
		if !exists {
			continue // Skip if transaction not found
		}

		// Parse transaction to get details
		tx, err := utils.ParseTx(txData)
		if err != nil {
			continue // Skip if parsing fails
		}
		// Create pending transaction info
		pendingTx := &pb.TransactionData{
			TxHash:    txHash,
			Sender:    tx.Sender,
			Recipient: tx.Recipient,
			Amount:    utils.Uint256ToString(tx.Amount),
			Nonce:     tx.Nonce,
			Timestamp: tx.Timestamp,
			Status:    pb.TransactionStatus_PENDING,
		}

		pendingTxs = append(pendingTxs, pendingTx)
	}

	return &pb.GetPendingTransactionsResponse{
		TotalCount: totalCount,
		PendingTxs: pendingTxs,
		Error:      "",
	}, nil
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
		}

	case *events.TransactionFailed:
		return &pb.TransactionStatusInfo{
			TxHash:        txHash,
			Status:        pb.TransactionStatus_FAILED,
			ErrorMessage:  e.ErrorMessage(),
			Confirmations: 0, // No confirmations for failed transactions
			Timestamp:     uint64(e.Timestamp().Unix()),
			ExtraInfo:     e.TxExtraInfo(),
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
		currentBlock = s.blockStore.GetLatestSlot()
	}

	return &pb.GetBlockNumberResponse{
		BlockNumber: currentBlock,
	}, nil
}

// GetBlockByNumber retrieves a block by its number
func (s *server) GetBlockByNumber(ctx context.Context, in *pb.GetBlockByNumberRequest) (*pb.GetBlockByNumberResponse, error) {
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
			tx, _, err := s.ledger.GetTxByHash(txHash)

			if err != nil {
				return nil, status.Errorf(codes.NotFound, "tx %s not found", txHash)
			}

			info, err := s.GetTransactionStatus(ctx, &pb.GetTransactionStatusRequest{TxHash: txHash})
			if err != nil {
				return nil, status.Errorf(codes.NotFound, "tx %s not found", txHash)
			}

			txStatus := info.Status
			blockTxs = append(blockTxs, &pb.TransactionData{
				TxHash:    txHash,
				Sender:    tx.Sender,
				Recipient: tx.Recipient,
				Amount:    utils.Uint256ToString(tx.Amount),
				Nonce:     tx.Nonce,
				Timestamp: tx.Timestamp,
				Status:    txStatus,
				TextData:  tx.TextData,
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

	return &pb.GetBlockByNumberResponse{
		Blocks:   blocks,
		Decimals: uint32(config.GetDecimalsFactor()),
	}, nil
}

// GetAccountByAddress is a convenience RPC under AccountService to fetch account info
func (s *server) GetAccountByAddress(ctx context.Context, in *pb.GetAccountByAddressRequest) (*pb.GetAccountByAddressResponse, error) {
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
