package p2p

const (
	NodeInfoProtocol  = "/node-info"
	BlockSyncProtocol = "/block-sync"

	TopicBlocks            = "blocks"
	TopicVotes             = "votes"
	TopicTxs               = "transactions"
	BlockSyncRequestTopic  = "block-sync/request"
	BlockSyncResponseTopic = "block-sync/response"
	LatestSlotTopic        = "latest-slot/request"
	AdvertiseName          = "mmn"
)

var (
	ConnCount int32  = 0
	MaxPeers  int32  = 50
	BatchSize uint64 = 1000
)
