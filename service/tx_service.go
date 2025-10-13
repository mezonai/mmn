package service

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/mezonai/mmn/abuse"
	"github.com/mezonai/mmn/errors"
	"github.com/mezonai/mmn/monitoring"
	"github.com/mezonai/mmn/ratelimit"

	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	pb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/utils"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type TxServiceImpl struct {
	ledger        *ledger.Ledger
	mempool       *mempool.Mempool
	blockStore    store.BlockStore
	tracker       interfaces.TransactionTrackerInterface
	rateLimiter   *ratelimit.GlobalRateLimiter
	abuseDetector *abuse.AbuseDetector
}

func NewTxService(ld *ledger.Ledger, mp *mempool.Mempool, bs store.BlockStore, tracker interfaces.TransactionTrackerInterface) *TxServiceImpl {
	return &TxServiceImpl{ledger: ld, mempool: mp, blockStore: bs, tracker: tracker}
}

func NewTxServiceWithProtection(ld *ledger.Ledger, mp *mempool.Mempool, bs store.BlockStore, tracker interfaces.TransactionTrackerInterface, rateLimiter *ratelimit.GlobalRateLimiter, abuseDetector *abuse.AbuseDetector) *TxServiceImpl {
	return &TxServiceImpl{
		ledger:        ld,
		mempool:       mp,
		blockStore:    bs,
		tracker:       tracker,
		rateLimiter:   rateLimiter,
		abuseDetector: abuseDetector,
	}
}

func extractClientIP(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "unknown"
	}

	addr := p.Addr.String()
	if addr == "" {
		return "unknown"
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		if net.ParseIP(addr) != nil {
			return addr
		}
		return "unknown"
	}

	// Handle IPv6 addresses that might be wrapped in brackets
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}

	return host
}

func extractWalletFromTx(tx *transaction.Transaction) string {
	if tx == nil || tx.Sender == "" {
		return "unknown"
	}
	return tx.Sender
}

func (s *TxServiceImpl) AddTx(ctx context.Context, in *pb.SignedTxMsg) (*pb.AddTxResponse, error) {
	logx.Debug("GRPC", fmt.Sprintf("received tx %+v", in.TxMsg))
	tx, err := utils.FromProtoSignedTx(in)
	monitoring.IncreaseReceivedClientTxCount()
	if err != nil {
		logx.Error("GRPC ADD TX", "FromProtoSignedTx error ", err)
		return &pb.AddTxResponse{Ok: false, Error: "invalid tx"}, nil
	}

	// Generate server-side timestamp for security
	// Todo: remove from input and update client
	tx.Timestamp = uint64(time.Now().UnixNano() / int64(time.Millisecond))

	clientIP := extractClientIP(ctx)
	walletAddr := extractWalletFromTx(tx)

	if s.rateLimiter != nil {
		if !s.rateLimiter.AllowAllWithContext(ctx, clientIP, walletAddr) {
			logx.Warn("RATE_LIMIT", "Rate limit exceeded for IP:", clientIP, "Wallet:", walletAddr)
			return &pb.AddTxResponse{
				Ok:    false,
				Error: "Rate limit exceeded. Please try again later.",
			}, status.Errorf(codes.ResourceExhausted, "rate limit exceeded")
		}
	}

	if s.abuseDetector != nil {
		if err := s.abuseDetector.CheckTransactionRate(clientIP, walletAddr); err != nil {
			logx.Warn("ABUSE_DETECTION", "Abuse detected for IP:", clientIP, "Wallet:", walletAddr, "Error:", err.Error())
			return &pb.AddTxResponse{
				Ok:    false,
				Error: "Transaction rejected due to abuse detection. Please contact support if this is an error.",
			}, status.Errorf(codes.PermissionDenied, "abuse detected: %v", err)
		}
	}

	txHash, err := s.mempool.AddTx(tx, true)
	if err != nil {
		return &pb.AddTxResponse{Ok: false, Error: err.Error()}, nil
	}
	return &pb.AddTxResponse{Ok: true, TxHash: txHash}, nil
}

func (s *TxServiceImpl) GetAbuseMetrics() *abuse.AbuseMetrics {
	if s.abuseDetector == nil {
		return &abuse.AbuseMetrics{}
	}
	return s.abuseDetector.GetMetrics()
}

func (s *TxServiceImpl) GetAbuseStats() *abuse.RateStats {
	if s.abuseDetector == nil {
		return &abuse.RateStats{}
	}
	return s.abuseDetector.GetRateStats()
}

func (s *TxServiceImpl) GetFlaggedIPs() map[string]*abuse.AbuseFlag {
	if s.abuseDetector == nil {
		return make(map[string]*abuse.AbuseFlag)
	}
	return s.abuseDetector.GetFlaggedIPs()
}

func (s *TxServiceImpl) GetFlaggedWallets() map[string]*abuse.AbuseFlag {
	if s.abuseDetector == nil {
		return make(map[string]*abuse.AbuseFlag)
	}
	return s.abuseDetector.GetFlaggedWallets()
}

func (s *TxServiceImpl) GetTxByHash(ctx context.Context, in *pb.GetTxByHashRequest) (*pb.GetTxByHashResponse, error) {
	tx, txMeta, errTx, errTxMeta := s.ledger.GetTxByHash(in.TxHash)
	if errTx != nil || errTxMeta != nil {
		logx.Error("GRPC GET TX", "Ledger error", fmt.Sprintf("errTx: %v, errTxMeta: %v", errTx, errTxMeta))
		return &pb.GetTxByHashResponse{Error: errors.NewError(errors.ErrCodeTransactionNotFound, errors.ErrMsgTransactionNotFound).Error()}, nil
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

func (s *TxServiceImpl) GetTransactionStatus(ctx context.Context, in *pb.GetTransactionStatusRequest) (*pb.TransactionStatusInfo, error) {
	txHash := in.TxHash

	// 1) Check mempool
	if s.mempool != nil {
		if data, ok := s.mempool.GetTransaction(txHash); ok {
			tx, err := utils.ParseTx(data)
			if err == nil && tx.Hash() == txHash {
				return &pb.TransactionStatusInfo{
					TxHash:        txHash,
					Status:        pb.TransactionStatus_PENDING,
					Confirmations: 0, // No confirmations for mempool transactions
					Timestamp:     uint64(time.Now().Unix()),
					ExtraInfo:     tx.ExtraInfo,
					Amount:        utils.Uint256ToString(tx.Amount),
					TextData:      tx.TextData,
				}, nil
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
			tx, _, errTx, errTxMeta := s.ledger.GetTxByHash(txHash)
			if errTx != nil || errTxMeta != nil {
				logx.Error("GRPC GET TX STATUS", "Ledger error", fmt.Sprintf("errTx: %v, errTxMeta: %v", errTx, errTxMeta))
				return nil, errors.NewError(errors.ErrCodeTransactionNotFound, errors.ErrMsgTransactionNotFound)
			}
			return &pb.TransactionStatusInfo{
				TxHash:        txHash,
				Status:        status,
				BlockSlot:     slot,
				BlockHash:     blk.HashString(),
				Confirmations: confirmations,
				Timestamp:     uint64(time.Now().Unix()),
				ExtraInfo:     tx.ExtraInfo,
				Amount:        utils.Uint256ToString(tx.Amount),
				TextData:      tx.TextData,
			}, nil
		}
	}

	// 3) Transaction not found anywhere -> return nil and error
	return nil, errors.NewError(errors.ErrCodeTransactionNotFound, errors.ErrMsgTransactionNotFound)
}

func (s *TxServiceImpl) GetPendingTransactions(ctx context.Context, in *pb.GetPendingTransactionsRequest) (*pb.GetPendingTransactionsResponse, error) {
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
