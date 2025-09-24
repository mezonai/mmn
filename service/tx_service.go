package service

import (
	"context"
	"fmt"
	"time"

	"github.com/mezonai/mmn/monitoring"

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
	ledger     *ledger.Ledger
	mempool    *mempool.Mempool
	blockStore store.BlockStore
	tracker    interfaces.TransactionTrackerInterface
}

func NewTxService(ld *ledger.Ledger, mp *mempool.Mempool, bs store.BlockStore, tracker interfaces.TransactionTrackerInterface) *TxServiceImpl {
	return &TxServiceImpl{ledger: ld, mempool: mp, blockStore: bs, tracker: tracker}
}

func (s *TxServiceImpl) AddTx(ctx context.Context, in *pb.SignedTxMsg) (*pb.AddTxResponse, error) {
	logx.Info("GRPC", fmt.Sprintf("received tx %+v", in.TxMsg))
	tx, err := utils.FromProtoSignedTx(in)
	monitoring.IncreaseReceivedClientTxCount()
	if err != nil {
		logx.Error("GRPC ADD TX", "FromProtoSignedTx error ", err)
		return &pb.AddTxResponse{Ok: false, Error: "invalid tx"}, nil
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

func (s *TxServiceImpl) GetTxByHash(ctx context.Context, in *pb.GetTxByHashRequest) (*pb.GetTxByHashResponse, error) {
	tx, txMeta, errTx, errTxMeta := s.ledger.GetTxByHash(in.TxHash)
	if errTx != nil || errTxMeta != nil {
		err := fmt.Errorf("error while retrieving tx by hash: %v, %v", errTx, errTxMeta)
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
				err := fmt.Errorf("error while retrieving tx by hash: %v, %v", errTx, errTxMeta)
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
				Amount:        utils.Uint256ToString(tx.Amount),
				TextData:      tx.TextData,
			}, nil
		}
	}

	// 3) Transaction not found anywhere -> return nil and error
	return nil, fmt.Errorf("transaction not found: %s", txHash)
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
