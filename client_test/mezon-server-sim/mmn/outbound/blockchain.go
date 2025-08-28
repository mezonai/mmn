package outbound

import (
	"context"

	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/domain"
	mmnpb "github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/proto"
)

type MainnetClient interface {
	AddTx(tx domain.SignedTx) (domain.AddTxResponse, error)
	GetAccount(addr string) (domain.Account, error)
	GetTxHistory(addr string, limit, offset, filter int) (domain.TxHistoryResponse, error)
	SubscribeTransactionStatus(ctx context.Context) (mmnpb.TxService_SubscribeTransactionStatusClient, error)
	GetTxByHash(txHash string) (domain.TxInfo, error)
}
