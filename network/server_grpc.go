package network

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"github.com/mezonai/mmn/store"
	"net"
	"time"

	"github.com/mezonai/mmn/consensus"
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
	blockStore    store.BlockStore
	mempool       *mempool.Mempool
}

func NewGRPCServer(addr string, pubKeys map[string]ed25519.PublicKey, blockDir string,
	ld *ledger.Ledger, collector *consensus.Collector,
	selfID string, priv ed25519.PrivateKey, validator *validator.Validator, blockStore store.BlockStore, mempool *mempool.Mempool) *grpc.Server {

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

	// Generate server-side timestamp for security
	// Todo: remove from input and update client
	tx.Timestamp = uint64(time.Now().UnixNano() / int64(time.Millisecond))

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
	senderAccount, err := s.ledger.GetAccount(tx.Sender)
	if err != nil {
		fmt.Printf("[gRPC] error while retriving account to add tx: %v\n", err)
		return &pb.AddTxResponse{Ok: false, Error: "failed to get account"}, nil
	}
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

func (s *server) GetTxByHash(ctx context.Context, in *pb.GetTxByHashRequest) (*pb.GetTxByHashResponse, error) {
	tx, err := s.ledger.GetTxByHash(in.TxHash)
	if err != nil {
		return &pb.GetTxByHashResponse{Error: err.Error()}, nil
	}
	txInfo := &pb.TxInfo{
		Sender:    tx.Sender,
		Recipient: tx.Recipient,
		Amount:    tx.Amount,
		Timestamp: tx.Timestamp,
		TextData:  tx.TextData,
	}
	return &pb.GetTxByHashResponse{Tx: txInfo}, nil
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
	
	timestamp := uint64(time.Now().Unix())
	
	return &pb.GetBlockNumberResponse{
		BlockNumber: currentBlock,
		Timestamp:   timestamp,
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
			tx, err := s.ledger.GetTxByHash(txHash)
			if err != nil {
				return nil, status.Errorf(codes.NotFound, "tx %s not found", txHash)
			}
			blockTxs = append(blockTxs, &pb.TransactionData{
				TxHash:    txHash,
				Sender:    tx.Sender,
				Recipient: tx.Recipient,
				Amount:    tx.Amount,
				Nonce:     tx.Nonce,
				Timestamp: tx.Timestamp,
				Status:    pb.TransactionData_CONFIRMED,
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

	return &pb.GetBlockByNumberResponse{Blocks: blocks}, nil
}
