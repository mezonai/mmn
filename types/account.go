package types

type TxRecord struct {
	Slot      uint64
	Amount    uint64
	Sender    string
	Recipient string
	Timestamp uint64
	TextData  string
	Type      int32
	Nonce     uint64
}

type Account struct {
	Address string
	Balance uint64
	Nonce   uint64
	History []string // tx hashes
}

type SnapshotAccount struct {
	Balance uint64
	Nonce   uint64
}
