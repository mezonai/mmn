package p2p

const (
	NodeInfoProtocol       = "/node-info"
	RequestBlockSyncStream = "/sync-block-request"
	LatestSlotProtocol     = "/latest-slot-request"
	CheckpointProtocol     = "/checkpoint-hash"

	TopicBlocks            = "blocks"
	TopicVotes             = "votes"
	TopicTxs               = "transactions"
	BlockSyncRequestTopic  = "block-sync/request"
	BlockSyncResponseTopic = "block-sync/response"
	LatestSlotTopic        = "latest-slot/request"
	CheckpointRequestTopic = "checkpoint/request"
	TopicSnapshotAnnounce  = "snapshot/announce"
	TopicSnapshotRequest   = "snapshot/request"
	AdvertiseName          = "mmn"
)

var (
	ConnCount         int32  = 0
	MaxPeers          int32  = 50
	BatchSize         uint64 = 10 // for test only, should be 1000
	MaxScanRange      uint64 = 100
	RangeForSnapshot  uint64 = 50
	ReadyGapThreshold uint64 = 5
)
