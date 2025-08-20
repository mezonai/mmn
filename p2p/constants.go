package p2p

const (
	NodeInfoProtocol       = "/node-info"
	RequestBlockSyncStream = "/sync-block-request"
	LatestSlotProtocol     = "/latest-slot-request"
	AuthProtocol           = "/auth/handshake/1.0.0"

	TopicBlocks            = "blocks"
	TopicVotes             = "votes"
	TopicTxs               = "transactions"
	BlockSyncRequestTopic  = "block-sync/request"
	BlockSyncResponseTopic = "block-sync/response"
	LatestSlotTopic        = "latest-slot/request"
	AdvertiseName          = "mmn"

	AuthChallengeSize = 32
	AuthTimeout       = 600 // 10 minutes
)

var (
	ConnCount int32  = 0
	MaxPeers  int32  = 50
	BatchSize uint64 = 5 // for test only, should be 1000
)
