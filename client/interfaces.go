package client

import (
	"context"

	mmnpb "github.com/mezonai/mmn/proto"
	"google.golang.org/grpc"
)

type MainnetClient interface {
	AddTx(ctx context.Context, tx SignedTx) (AddTxResponse, error)
	GetAccount(ctx context.Context, addr string) (Account, error)
	GetTxHistory(ctx context.Context, addr string, limit, offset, filter int) (TxHistoryResponse, error)
	SubscribeTransactionStatus(ctx context.Context) (mmnpb.TxService_SubscribeTransactionStatusClient, error)
	GetTxByHash(ctx context.Context, txHash string) (TxInfo, error)
	CheckHealth(ctx context.Context) (*mmnpb.HealthCheckResponse, error)
	Conn() *grpc.ClientConn
	Close() error
}

type WalletManager interface {
	LoadKey(userID uint64) (addr string, privKey []byte, err error)
	CreateKey(userID uint64) (addr string, privKey []byte, err error)
}
