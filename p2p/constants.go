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

	// Shred topics for optimized block broadcasting
	TopicShreds         = "shreds"
	TopicRepairRequest  = "repair/request"
	TopicRepairResponse = "repair/response"

	AdvertiseName = "mmn"
)

var (
	ConnCount int32 = 0
	MaxPeers  int32 = 50
)
