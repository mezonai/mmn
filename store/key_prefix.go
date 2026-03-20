package store

// Declare database key prefix for objects
const (
	PrefixAccount = "account:"

	PrefixBlockMeta             = "blk_meta:"
	PrefixBlock                 = "blk:"
	BlockMetaKeyLatestFinalized = "latest_finalized"
	BlockMetaKeyLatestStore     = "latest_store"
	BlockMetaKeyLatestHeight    = "latest_height"

	PrefixSlot          = "slot:"
	PrefixSlotFinalized = "slot_finalized:"
	PrefixHeightToSlot  = "height_to_slot:"

	PrefixTx     = "tx:"
	PrefixTxMeta = "tx_meta:"

	PrefixLatestVersionContent = "latest_version_content:"
)
