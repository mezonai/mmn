package network

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net"
	"time"

	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/errors"
	"github.com/mezonai/mmn/faucet"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/types"

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
	pb.UnimplementedTxServiceServer
	pb.UnimplementedAccountServiceServer
	pb.UnimplementedHealthServiceServer
	pubKeys           map[string]ed25519.PublicKey
	blockDir          string
	ledger            *ledger.Ledger
	voteCollector     *consensus.Collector
	selfID            string
	privKey           ed25519.PrivateKey
	validator         *validator.Validator
	blockStore        store.BlockStore
	mempool           *mempool.Mempool
	eventRouter       *events.EventRouter                    // Event router for complex event logic
	txTracker         interfaces.TransactionTrackerInterface // Transaction state tracker
	txSvc             interfaces.TxService
	acctSvc           interfaces.AccountService
	multisigFaucetSvc *faucet.MultisigFaucetService
}

func NewGRPCServer(addr string, pubKeys map[string]ed25519.PublicKey, blockDir string,
	ld *ledger.Ledger, collector *consensus.Collector,
	selfID string, priv ed25519.PrivateKey, validator *validator.Validator, blockStore store.BlockStore, mempool *mempool.Mempool, eventRouter *events.EventRouter, txTracker interfaces.TransactionTrackerInterface, multisigFaucetSvc *faucet.MultisigFaucetService) *grpc.Server {

	s := &server{
		pubKeys:           pubKeys,
		blockDir:          blockDir,
		ledger:            ld,
		voteCollector:     collector,
		selfID:            selfID,
		privKey:           priv,
		blockStore:        blockStore,
		validator:         validator,
		mempool:           mempool,
		eventRouter:       eventRouter,
		txTracker:         txTracker,
		multisigFaucetSvc: multisigFaucetSvc,
	}

	// Initialize shared services
	s.txSvc = service.NewTxService(ld, mempool, blockStore, txTracker)
	s.acctSvc = service.NewAccountService(ld, mempool, txTracker)

	grpcSrv := grpc.NewServer()
	pb.RegisterBlockServiceServer(grpcSrv, s)
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
	errors := make([]string, 0)

	for _, slot := range slots {
		block, exists := blockMap[slot]
		if !exists {
			logx.Error("GRPC SERVER", fmt.Sprintf("Block %d not found, skipping", slot))
			errors = append(errors, fmt.Sprintf("Block %d not found, skipping", slot))
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
				errors = append(errors, fmt.Sprintf("Transaction or meta not found for tx %s in block %d", txHash, slot))
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
		Errors:      errors,
	}, nil
}

// Multisig Faucet Methods

// CreateFaucetRequest creates a new multisig faucet request
func (s *server) CreateFaucetRequest(ctx context.Context, req *pb.CreateFaucetRequestRequest) (*pb.CreateFaucetRequestResponse, error) {
	if s.multisigFaucetSvc == nil {
		return &pb.CreateFaucetRequestResponse{
			Success: false,
			Message: "Multisig faucet service not initialized",
		}, nil
	}

	logx.Info("MultisigFaucetGRPC", "CreateFaucetRequest called",
		"multisigAddress", req.MultisigAddress,
		"recipient", req.Recipient,
		"amount", req.Amount)

	// Parse amount
	amount := utils.Uint256FromString(req.Amount)

	// Decode signature
	signature, err := hex.DecodeString(req.Signature)
	if err != nil {
		return &pb.CreateFaucetRequestResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid signature format: %v", err),
		}, nil
	}

	// Create faucet request
	tx, err := s.multisigFaucetSvc.CreateFaucetRequest(
		req.MultisigAddress,
		amount,
		req.TextData,
		req.SignerPubkey,
		signature,
	)
	if err != nil {
		return &pb.CreateFaucetRequestResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to create faucet request: %v", err),
		}, nil
	}

	return &pb.CreateFaucetRequestResponse{
		Success: true,
		Message: "Faucet request created successfully",
		TxHash:  tx.Hash(),
	}, nil
}

// CheckWhitelistStatus checks if an address is in whitelist
func (s *server) CheckWhitelistStatus(ctx context.Context, req *pb.CheckWhitelistStatusRequest) (*pb.CheckWhitelistStatusResponse, error) {
	if s.multisigFaucetSvc == nil {
		return &pb.CheckWhitelistStatusResponse{
			Success: false,
			Message: "Multisig faucet service not initialized",
		}, nil
	}

	logx.Info("MultisigFaucetGRPC", "CheckWhitelistStatus called",
		"address", req.Address)

	isApprover := s.multisigFaucetSvc.IsApprover(req.Address)
	isProposer := s.multisigFaucetSvc.IsProposer(req.Address)

	return &pb.CheckWhitelistStatusResponse{
		Success:    true,
		Message:    "Whitelist status retrieved",
		IsApprover: isApprover,
		IsProposer: isProposer,
	}, nil
}

// AddSignature adds a signature to a multisig transaction
func (s *server) AddSignature(ctx context.Context, req *pb.AddSignatureRequest) (*pb.AddSignatureResponse, error) {
	if s.multisigFaucetSvc == nil {
		return &pb.AddSignatureResponse{
			Success: false,
			Message: "Multisig faucet service not initialized",
		}, nil
	}
	// Decode signature
	signature, err := hex.DecodeString(req.Signature)
	if err != nil {
		return &pb.AddSignatureResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid signature format: %v", err),
		}, nil
	}

	// Add signature
	err = s.multisigFaucetSvc.AddSignature(req.TxHash, req.SignerPubkey, signature)
	if err != nil {
		return &pb.AddSignatureResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to add signature: %v", err),
		}, nil
	}

	// Get updated transaction to count signatures
	tx, err := s.multisigFaucetSvc.GetMultisigTx(req.TxHash)
	if err != nil {
		return &pb.AddSignatureResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to get transaction: %v", err),
		}, nil
	}

	return &pb.AddSignatureResponse{
		Success:        true,
		Message:        "Signature added successfully",
		SignatureCount: int32(len(tx.Signatures)),
	}, nil
}

// RejectProposal rejects a multisig transaction proposal
func (s *server) RejectProposal(ctx context.Context, req *pb.RejectProposalRequest) (*pb.RejectProposalResponse, error) {
	if s.multisigFaucetSvc == nil {
		return &pb.RejectProposalResponse{
			Success: false,
			Message: "Multisig faucet service not initialized",
		}, nil
	}

	logx.Info("MultisigFaucetGRPC", "RejectProposal called",
		"txHash", req.TxHash)

	// Decode signature
	signature, err := hex.DecodeString(req.Signature)
	if err != nil {
		return &pb.RejectProposalResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid signature format: %v", err),
		}, nil
	}

	// Reject proposal
	err = s.multisigFaucetSvc.RejectProposal(req.TxHash, req.SignerPubkey, signature)
	if err != nil {
		return &pb.RejectProposalResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to reject proposal: %v", err),
		}, nil
	}

	return &pb.RejectProposalResponse{
		Success: true,
		Message: "Proposal rejected successfully",
	}, nil
}

// GetMultisigTransactionStatus gets the status of a multisig transaction
func (s *server) GetMultisigTransactionStatus(ctx context.Context, req *pb.GetMultisigTransactionStatusRequest) (*pb.GetMultisigTransactionStatusResponse, error) {
	if s.multisigFaucetSvc == nil {
		return &pb.GetMultisigTransactionStatusResponse{
			Success: false,
			Message: "Multisig faucet service not initialized",
		}, nil
	}

	logx.Info("MultisigFaucetGRPC", "GetMultisigTransactionStatus called",
		"txHash", req.TxHash)

	// Get transaction
	tx, err := s.multisigFaucetSvc.GetMultisigTx(req.TxHash)
	if err != nil {
		return &pb.GetMultisigTransactionStatusResponse{
			Success: false,
			Message: fmt.Sprintf("Transaction not found: %v", err),
		}, nil
	}

	// Determine status
	status := tx.Status
	if status == "" {
		status = "pending"
	}

	// Override status based on signature count if not executed
	if status != "executed" && status != "failed" {
		if len(tx.Signatures) >= tx.Config.Threshold {
			// Check if transaction is still in pending (not executed yet)
			if _, exists := s.multisigFaucetSvc.GetPendingTxs()[req.TxHash]; exists {
				status = "ready_to_execute"
			} else {
				status = "executed"
			}
		} else {
			status = "pending"
		}
	}

	return &pb.GetMultisigTransactionStatusResponse{
		Success:            true,
		Message:            tx.TextData,
		Status:             status,
		SignatureCount:     int32(len(tx.Signatures)),
		RequiredSignatures: int32(tx.Config.Threshold),
	}, nil
}

// AddToApproverWhitelist adds an address to approver whitelist
func (s *server) AddToApproverWhitelist(ctx context.Context, req *pb.AddToApproverWhitelistRequest) (*pb.AddToApproverWhitelistResponse, error) {
	if s.multisigFaucetSvc == nil {
		return &pb.AddToApproverWhitelistResponse{
			Success: false,
			Message: "Multisig faucet service not initialized",
		}, nil
	}

	logx.Info("MultisigFaucetGRPC", "AddToApproverWhitelist called",
		"address", req.Address,
		"signerPubkey", req.SignerPubkey)

	// Decode signature
	signature, err := hex.DecodeString(req.Signature)
	if err != nil {
		return &pb.AddToApproverWhitelistResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid signature format: %v", err),
		}, nil
	}

	// Add to whitelist
	err = s.multisigFaucetSvc.AddToApproverWhitelist(req.Address, req.SignerPubkey, signature)
	if err != nil {
		return &pb.AddToApproverWhitelistResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to add to approver whitelist: %v", err),
		}, nil
	}

	return &pb.AddToApproverWhitelistResponse{
		Success: true,
		Message: "Address added to approver whitelist successfully",
	}, nil
}

// AddToProposerWhitelist adds an address to proposer whitelist
func (s *server) AddToProposerWhitelist(ctx context.Context, req *pb.AddToProposerWhitelistRequest) (*pb.AddToProposerWhitelistResponse, error) {
	if s.multisigFaucetSvc == nil {
		return &pb.AddToProposerWhitelistResponse{
			Success: false,
			Message: "Multisig faucet service not initialized",
		}, nil
	}

	logx.Info("MultisigFaucetGRPC", "AddToProposerWhitelist called",
		"address", req.Address,
		"signerPubkey", req.SignerPubkey)

	// Decode signature
	signature, err := hex.DecodeString(req.Signature)
	if err != nil {
		return &pb.AddToProposerWhitelistResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid signature format: %v", err),
		}, nil
	}

	// Add to whitelist
	err = s.multisigFaucetSvc.AddToProposerWhitelist(req.Address, req.SignerPubkey, signature)
	if err != nil {
		return &pb.AddToProposerWhitelistResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to add to proposer whitelist: %v", err),
		}, nil
	}

	return &pb.AddToProposerWhitelistResponse{
		Success: true,
		Message: "Address added to proposer whitelist successfully",
	}, nil
}

// RemoveFromApproverWhitelist removes an address from approver whitelist
func (s *server) RemoveFromApproverWhitelist(ctx context.Context, req *pb.RemoveFromApproverWhitelistRequest) (*pb.RemoveFromApproverWhitelistResponse, error) {
	if s.multisigFaucetSvc == nil {
		return &pb.RemoveFromApproverWhitelistResponse{
			Success: false,
			Message: "Multisig faucet service not initialized",
		}, nil
	}

	logx.Info("MultisigFaucetGRPC", "RemoveFromApproverWhitelist called",
		"address", req.Address,
		"signerPubkey", req.SignerPubkey)

	// Decode signature
	signature, err := hex.DecodeString(req.Signature)
	if err != nil {
		return &pb.RemoveFromApproverWhitelistResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid signature format: %v", err),
		}, nil
	}

	// Remove from whitelist
	err = s.multisigFaucetSvc.RemoveFromApproverWhitelist(req.Address, req.SignerPubkey, signature)
	if err != nil {
		return &pb.RemoveFromApproverWhitelistResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to remove from approver whitelist: %v", err),
		}, nil
	}

	return &pb.RemoveFromApproverWhitelistResponse{
		Success: true,
		Message: "Address removed from approver whitelist successfully",
	}, nil
}

// RemoveFromProposerWhitelist removes an address from proposer whitelist
func (s *server) RemoveFromProposerWhitelist(ctx context.Context, req *pb.RemoveFromProposerWhitelistRequest) (*pb.RemoveFromProposerWhitelistResponse, error) {
	if s.multisigFaucetSvc == nil {
		return &pb.RemoveFromProposerWhitelistResponse{
			Success: false,
			Message: "Multisig faucet service not initialized",
		}, nil
	}

	logx.Info("MultisigFaucetGRPC", "RemoveFromProposerWhitelist called",
		"address", req.Address,
		"signerPubkey", req.SignerPubkey)

	// Decode signature
	signature, err := hex.DecodeString(req.Signature)
	if err != nil {
		return &pb.RemoveFromProposerWhitelistResponse{
			Success: false,
			Message: fmt.Sprintf("Invalid signature format: %v", err),
		}, nil
	}

	// Remove from whitelist
	err = s.multisigFaucetSvc.RemoveFromProposerWhitelist(req.Address, req.SignerPubkey, signature)
	if err != nil {
		return &pb.RemoveFromProposerWhitelistResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to remove from proposer whitelist: %v", err),
		}, nil
	}

	return &pb.RemoveFromProposerWhitelistResponse{
		Success: true,
		Message: "Address removed from proposer whitelist successfully",
	}, nil
}

// GetApproverWhitelist gets the list of approver whitelist
func (s *server) GetApproverWhitelist(ctx context.Context, req *pb.GetApproverWhitelistRequest) (*pb.GetApproverWhitelistResponse, error) {
	if s.multisigFaucetSvc == nil {
		return &pb.GetApproverWhitelistResponse{
			Success: false,
			Message: "Multisig faucet service not initialized",
		}, nil
	}

	logx.Info("MultisigFaucetGRPC", "GetApproverWhitelist called")

	// Get approver whitelist
	addresses := s.multisigFaucetSvc.GetApproverWhitelist()

	return &pb.GetApproverWhitelistResponse{
		Success:   true,
		Message:   "Approver whitelist retrieved successfully",
		Addresses: addresses,
	}, nil
}

// GetProposerWhitelist gets the list of proposer whitelist
func (s *server) GetProposerWhitelist(ctx context.Context, req *pb.GetProposerWhitelistRequest) (*pb.GetProposerWhitelistResponse, error) {
	if s.multisigFaucetSvc == nil {
		return &pb.GetProposerWhitelistResponse{
			Success: false,
			Message: "Multisig faucet service not initialized",
		}, nil
	}

	logx.Info("MultisigFaucetGRPC", "GetProposerWhitelist called")

	// Get proposer whitelist
	addresses := s.multisigFaucetSvc.GetProposerWhitelist()

	return &pb.GetProposerWhitelistResponse{
		Success:   true,
		Message:   "Proposer whitelist retrieved successfully",
		Addresses: addresses,
	}, nil
}
