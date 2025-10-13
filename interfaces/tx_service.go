package interfaces

import (
	"context"

	pb "github.com/mezonai/mmn/proto"
)

type TxService interface {
	AddTx(ctx context.Context, in *pb.SignedTxMsg) (*pb.AddTxResponse, error)
	GetTxByHash(ctx context.Context, in *pb.GetTxByHashRequest) (*pb.GetTxByHashResponse, error)
	GetTransactionStatus(ctx context.Context, in *pb.GetTransactionStatusRequest) (*pb.TransactionStatusInfo, error)
	GetPendingTransactions(ctx context.Context, in *pb.GetPendingTransactionsRequest) (*pb.GetPendingTransactionsResponse, error)
}
