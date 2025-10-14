package p2p

import "time"

const (
	NodeInfoProtocol       = "/node-info"
	RequestBlockSyncStream = "/sync-block-request"
	LatestSlotProtocol     = "/latest-slot-request"
	CheckpointProtocol     = "/checkpoint-hash"
	SnapshotSyncProtocol   = "/snapshot-sync"

	TopicBlocks            = "blocks"
	TopicVotes             = "votes"
	TopicTxs               = "transactions"
	TopicEmptyBlocks       = "block-sync/empty-blocks"
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
	ConnCount                       int32         = 0
	MaxPeers                        int32         = 50
	SyncBlocksBatchSize             uint64        = 100
	MaxcheckpointScanBlocksRange    uint64        = 100
	ReadyGapThreshold               uint64        = 0
	LatestSlotSyncGapThreshold      uint64        = 1
	WaitWorldLatestSlotTimeInterval time.Duration = 50 * time.Millisecond
	SnapshotChunkSize               int           = 16384
	SnapshotReadyGapThreshold       uint64        = 2
	SnapshotRangeFor                uint64        = 100 // should be 20k ~ 50k
	InitRequestLatestSlotMaxRetries int           = 3
)
