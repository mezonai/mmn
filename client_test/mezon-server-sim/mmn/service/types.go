package service

type WalletLedgerList struct {
	WalletLedger []*WalletLedger
	Count        int32
}

type WalletLedger struct {
	Id            string
	UserId        string
	CreateTime    uint64
	Value         int32
	TransactionId string
}
