package service

import "github.com/holiman/uint256"

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
