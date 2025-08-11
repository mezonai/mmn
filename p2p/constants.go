package p2p

const (
	BlockProtocol    = "/block"
	VoteProtocol     = "/vote"
	TxProtocol       = "/tx"
	NodeInfoProtocol = "/node-info"

	TopicBlocks            = "blocks"
	TopicVotes             = "votes"
	TopicTxs               = "transactions"
	BlockSyncRequestTopic  = "block-sync/request"
	BlockSyncResponseTopic = "block-sync/response"
	AdvertiseName          = "mmn"
)
