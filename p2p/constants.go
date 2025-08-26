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
	AccessControlTopic     = "access-control/update"
	AccessControlSyncTopic = "access-control/sync"
	AdvertiseName          = "mmn"

	AuthChallengeSize = 32
	AuthTimeout       = 600 // 10 minutes
)

var (
	P2pMaxPeerConnections   int32  = 50
	SyncBlockBatchSize      uint64 = 10 // for test only, should be 1000
	AuthLimitMessagePayload int64  = 2048
)
