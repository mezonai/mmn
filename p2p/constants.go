package p2p

const (
	NodeInfoProtocol       = "/node-info"
	RequestBlockSyncStream = "/sync-block-request"
	LatestSlotProtocol     = "/latest-slot-request"
	CheckpointProtocol     = "/checkpoint-hash"
	SnapshotSyncProtocol   = "/snapshot-sync"

	TopicBlocks            = "blocks"
	TopicVotes             = "votes"
	TopicTxs               = "transactions"
	BlockSyncRequestTopic  = "block-sync/request"
	BlockSyncResponseTopic = "block-sync/response"
	TopicSnapshotAnnounce  = "snapshot/announce"
	TopicSnapshotRequest   = "snapshot/request"
	LatestSlotTopic        = "latest-slot/request"
	CheckpointRequestTopic = "checkpoint/request"
	SnapshotRequestTopic   = "snapshot/request"
	SnapshotResponseTopic  = "snapshot/response"
	SnapshotSyncTopic      = "snapshot/sync"
	AdvertiseName          = "mmn"
)

var (
	ConnCount                    int32  = 0
	MaxPeers                     int32  = 50
	SyncBlocksBatchSize          uint64 = 10 // for test only, should be 1000
	MaxcheckpointScanBlocksRange uint64 = 100
	SnapshotRangeFor             uint64 = 50 // for test only, should be 20k ~ 50k
	SnapshotReadyGapThreshold    uint64 = 5
	SnapshotChunkSize            int    = 16384
)
