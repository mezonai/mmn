package client

import (
	"context"

	mmnpb "github.com/mezonai/mmn/proto"
	"google.golang.org/grpc"
)

type MainnetClient interface {
	AddTx(ctx context.Context, tx SignedTx) (AddTxResponse, error)
	GetAccount(ctx context.Context, addr string) (Account, error)
	SubscribeTransactionStatus(ctx context.Context) (mmnpb.TxService_SubscribeTransactionStatusClient, error)
	GetTxByHash(ctx context.Context, txHash string) (TxInfo, error)
	CheckHealth(ctx context.Context) (*mmnpb.HealthCheckResponse, error)
	GetCurrentNonce(ctx context.Context, addr string, tag string) (uint64, error)
	Conn() *grpc.ClientConn
	Close() error
}
