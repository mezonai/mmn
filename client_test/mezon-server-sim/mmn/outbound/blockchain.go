package outbound

import "mezon/v2/mmn/domain"

type MainnetClient interface {
	AddTx(tx domain.SignedTx) (domain.AddTxResponse, error)
	GetAccount(addr string) (domain.Account, error)
	GetTxHistory(addr string, limit, offset, filter int) (domain.TxHistoryResponse, error)
}
