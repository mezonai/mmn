package store

// Declare database key prefix for objects
const (
	PrefixAccount = "account:"

	PrefixBlockMeta = "blk_meta:"
	PrefixBlock     = "blk:"

	PrefixTx     = "tx:"
	PrefixTxMeta = "tx_meta:"

	PrefixStateMeta      = "state_meta:"
	PrefixBankHashBySlot = "state_meta:bank_hash:"
)
