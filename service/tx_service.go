package service

import (
	"context"
	"fmt"
	"time"

	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/interfaces"
	"github.com/mezonai/mmn/ledger"
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
	tx, err := utils.FromProtoSignedTx(in)
	if err != nil {
		return &pb.AddTxResponse{Ok: false, Error: "invalid tx"}, nil
	}
	// align with grpc: server-side timestamp
	tx.Timestamp = uint64(time.Now().UnixNano() / int64(time.Millisecond))
	// add
	txHash, err := s.mempool.AddTx(tx, true)
	if err != nil {
		return &pb.AddTxResponse{Ok: false, Error: err.Error()}, nil
	}
	return &pb.AddTxResponse{Ok: true, TxHash: txHash}, nil
}

func (s *TxServiceImpl) GetTxByHash(ctx context.Context, in *pb.GetTxByHashRequest) (*pb.GetTxByHashResponse, error) {
	tx, txMeta, err := s.ledger.GetTxByHash(in.TxHash)
	if err != nil {
		return &pb.GetTxByHashResponse{Error: err.Error()}, nil
	}
	amount := utils.Uint256ToString(tx.Amount)
	return &pb.GetTxByHashResponse{
		Tx: &pb.TxInfo{
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
		},
		Decimals: uint32(config.GetDecimalsFactor()),
	}, nil
}

func (s *TxServiceImpl) GetTransactionStatus(ctx context.Context, in *pb.GetTransactionStatusRequest) (*pb.TransactionStatusInfo, error) {
	txHash := in.TxHash
	if s.mempool != nil {
		if data, ok := s.mempool.GetTransaction(txHash); ok {
			tx, err := utils.ParseTx(data)
			if err == nil && tx.Hash() == txHash {
				return &pb.TransactionStatusInfo{TxHash: txHash, Status: pb.TransactionStatus_PENDING, Confirmations: 0, Timestamp: uint64(time.Now().Unix()), ExtraInfo: tx.ExtraInfo}, nil
			}
		}
	}
	if s.blockStore != nil {
		slot, blk, _, found := s.blockStore.GetTransactionBlockInfo(txHash)
		if found {
			confirmations := s.blockStore.GetConfirmations(slot)
			status := pb.TransactionStatus_CONFIRMED
			if confirmations > 1 { status = pb.TransactionStatus_FINALIZED }
			tx, _, err := s.ledger.GetTxByHash(txHash)
			if err != nil { return nil, err }
			return &pb.TransactionStatusInfo{TxHash: txHash, Status: status, BlockSlot: slot, BlockHash: blk.HashString(), Confirmations: confirmations, Timestamp: uint64(time.Now().Unix()), ExtraInfo: tx.ExtraInfo}, nil
		}
	}
	return nil, fmt.Errorf("transaction not found: %s", txHash)
}

func (s *TxServiceImpl) GetPendingTransactions(ctx context.Context, in *pb.GetPendingTransactionsRequest) (*pb.GetPendingTransactionsResponse, error) {
	if s.mempool == nil {
		return &pb.GetPendingTransactionsResponse{TotalCount: 0, PendingTxs: []*pb.TransactionData{}, Error: "mempool not available"}, nil
	}
	ordered := s.mempool.GetOrderedTransactions()
	var out []*pb.TransactionData
	for _, h := range ordered {
		if data, ok := s.mempool.GetTransaction(h); ok {
			tx, err := utils.ParseTx(data)
			if err != nil { continue }
			out = append(out, &pb.TransactionData{TxHash: h, Sender: tx.Sender, Recipient: tx.Recipient, Amount: utils.Uint256ToString(tx.Amount), Nonce: tx.Nonce, Timestamp: tx.Timestamp, Status: pb.TransactionStatus_PENDING, TextData: tx.TextData})
		}
	}
	return &pb.GetPendingTransactionsResponse{TotalCount: uint64(len(ordered)), PendingTxs: out}, nil
}


