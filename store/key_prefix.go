package store

// Declare database key prefix for objects
const (
	PrefixAccount = "account:"

	PrefixBlockMeta             = "blk_meta:"
	PrefixBlock                 = "blk:"
	PrefixBlockFinalized        = "blk_finalized:"
	BlockMetaKeyLatestFinalized = "latest_finalized"
	BlockMetaKeyLatestStore     = "latest_store"

	PrefixTx     = "tx:"
	PrefixTxMeta = "tx_meta:"

	PrefixLatestVersionFeed = "latest_version_feed:"
)
