package outbound

import (
	"context"
	"mmn/client_test/mezon-server-sim/mmn/domain"
	mmnpb "mmn/client_test/mezon-server-sim/mmn/proto"
)

type MainnetClient interface {
	AddTx(tx domain.SignedTx) (domain.AddTxResponse, error)
	GetAccount(addr string) (domain.Account, error)
	GetTxHistory(addr string, limit, offset, filter int) (domain.TxHistoryResponse, error)
	SubscribeTransactionStatus(ctx context.Context) (mmnpb.TxService_SubscribeTransactionStatusClient, error)
}
