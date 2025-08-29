package service

import "github.com/holiman/uint256"

const (
	UNLOCK_ITEM_STATUS_PENDING = 0
	UNLOCK_ITEM_STATUS_SUCCESS = 1
	UNLOCK_ITEM_STATUS_FAILED  = 2
)

type WalletLedgerList struct {
	WalletLedger []*WalletLedger
	Count        int32
}

type WalletLedger struct {
	Id            string
	UserId        string
	CreateTime    uint64
	Value         *uint256.Int
	TransactionId string
}
