package service

import (
	"context"
	"fmt"
	"time"

	"github.com/mezonai/mmn/errors"
	"github.com/mezonai/mmn/exception"
	"github.com/mezonai/mmn/monitoring"
	"github.com/mezonai/mmn/security/ratelimit"

	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	pb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/utils"
)

type TxServiceImpl struct {
	ledger      *ledger.Ledger
	mempool     *mempool.Mempool
	blockStore  store.BlockStore
	tracker     interfaces.TransactionTrackerInterface
	rateLimiter *ratelimit.GlobalRateLimiter
}

func NewTxService(ld *ledger.Ledger, mp *mempool.Mempool, bs store.BlockStore, tracker interfaces.TransactionTrackerInterface, rateLimiter *ratelimit.GlobalRateLimiter) *TxServiceImpl {
	return &TxServiceImpl{ledger: ld, mempool: mp, blockStore: bs, tracker: tracker, rateLimiter: rateLimiter}
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
	tx.Timestamp = uint64(time.Now().UnixNano() / int64(time.Millisecond))

	txHash, err := s.mempool.AddTx(tx, true)
	if err != nil {
		return &pb.AddTxResponse{Ok: false, Error: err.Error()}, nil
	}
	exception.SafeGo("TrackWalletRequest", func() {
		s.rateLimiter.TrackWalletRequest(tx.Sender)
	})
	return &pb.AddTxResponse{Ok: true, TxHash: txHash}, nil
}

func (s *TxServiceImpl) GetTxByHash(ctx context.Context, in *pb.GetTxByHashRequest) (*pb.GetTxByHashResponse, error) {
	txHash := in.TxHash
	decimalConfig := uint32(config.GetDecimalsFactor())

	// 1) Check ledger first to get the most accurate status (CONFIRMED/FINALIZED/FAILED).
	// This must come before mempool check because listener nodes do not call BlockCleanup,
	// so tx can remain in mempool even after being finalized — returning a stale PENDING.
	if s.ledger != nil {
		tx, txMeta, errTx, errTxMeta := s.ledger.GetTxByHash(txHash)
		if errTx == nil && errTxMeta == nil {
			txStatus := utils.TxMetaStatusToProtoTxStatus(txMeta.Status)

			// Lookup block height from blockStore using the slot stored in txMeta
			var blockHeight uint64
			if s.blockStore != nil && txMeta.Slot > 0 {
				if blk := s.blockStore.Block(txMeta.Slot); blk != nil {
					blockHeight = blk.Height
				}
			}

			txInfo := &pb.TxInfo{
				Sender:      tx.Sender,
				Recipient:   tx.Recipient,
				Amount:      utils.Uint256ToString(tx.Amount),
				Timestamp:   tx.Timestamp,
				TextData:    tx.TextData,
				Nonce:       tx.Nonce,
				Slot:        txMeta.Slot,
				BlockHash:   txMeta.BlockHash,
				Status:      txStatus,
				ErrMsg:      txMeta.Error,
				ExtraInfo:   tx.ExtraInfo,
				TxHash:      txHash,
				BlockHeight: blockHeight,
			}
			return &pb.GetTxByHashResponse{
				Tx:       txInfo,
				Decimals: decimalConfig,
			}, nil
		}
		if errTx != nil || errTxMeta != nil {
			logx.Error("GRPC GET TX STATUS", "Ledger error", fmt.Sprintf("errTx: %v, errTxMeta: %v", errTx, errTxMeta))
		}
	}

	// 2) Check mempool — tx is still pending (not yet included in any block)
	if s.mempool != nil {
		if tx, ok := s.mempool.GetTransaction(txHash); ok {
			txInfo := &pb.TxInfo{
				Sender:      tx.Sender,
				Recipient:   tx.Recipient,
				Amount:      utils.Uint256ToString(tx.Amount),
				Timestamp:   tx.Timestamp,
				TextData:    tx.TextData,
				Nonce:       tx.Nonce,
				Slot:        0,
				BlockHash:   "",
				Status:      pb.TransactionStatus_PENDING,
				ErrMsg:      "",
				ExtraInfo:   tx.ExtraInfo,
				TxHash:      txHash,
				BlockHeight: 0,
			}
			return &pb.GetTxByHashResponse{Tx: txInfo, Decimals: decimalConfig}, nil
		}
	}

	// 3) Last resort: check tracker — covers the brief window on the leader node where
	// tx has been pulled from mempool via PullBatch but AddBlockPending has not yet written it to DB.
	if s.tracker != nil {
		tx, err := s.tracker.GetTransaction(txHash)
		if err == nil {
			txInfo := &pb.TxInfo{
				Sender:      tx.Sender,
				Recipient:   tx.Recipient,
				Amount:      utils.Uint256ToString(tx.Amount),
				Timestamp:   tx.Timestamp,
				TextData:    tx.TextData,
				Nonce:       tx.Nonce,
				Slot:        0,
				BlockHash:   "",
				Status:      pb.TransactionStatus_PENDING,
				ErrMsg:      "",
				ExtraInfo:   tx.ExtraInfo,
				TxHash:      txHash,
				BlockHeight: 0,
			}
			return &pb.GetTxByHashResponse{Tx: txInfo, Decimals: decimalConfig}, nil
		}
	}

	// 4) Transaction not found anywhere
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
		tx, exists := s.mempool.GetTransaction(txHash)
		if !exists {
			continue // Skip if transaction not found
		}

		// Create pending transaction info
		pendingTx := &pb.TransactionData{
			TxHash:          txHash,
			Sender:          tx.Sender,
			Recipient:       tx.Recipient,
			Amount:          utils.Uint256ToString(tx.Amount),
			Nonce:           tx.Nonce,
			Timestamp:       tx.Timestamp,
			Status:          pb.TransactionStatus_PENDING,
			TransactionType: tx.Type,
		}

		pendingTxs = append(pendingTxs, pendingTx)
	}

	return &pb.GetPendingTransactionsResponse{
		TotalCount: totalCount,
		PendingTxs: pendingTxs,
		Error:      "",
	}, nil
}
