package p2p

import "time"

const (
	NodeInfoProtocol       = "/node-info"
	RequestBlockSyncStream = "/sync-block-request"
	LatestSlotProtocol     = "/latest-slot-request"
	AuthProtocol           = "/auth/handshake/1.0.0"
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
	AccessControlTopic     = "access-control/update"
	AccessControlSyncTopic = "access-control/sync"
	CheckpointRequestTopic = "checkpoint/request"
	SnapshotRequestTopic   = "snapshot/request"
	SnapshotResponseTopic  = "snapshot/response"
	SnapshotSyncTopic      = "snapshot/sync"
	AdvertiseName          = "mmn"

	AuthChallengeSize = 32
	AuthTimeout       = 600 // 10 minutes
)

var (
	P2pMaxPeerConnections           int32         = 50
	SyncBlockBatchSize              uint64        = 10 // for test only, should be 1000
	AuthLimitMessagePayload         int64         = 2048
	ConnCount                       int32         = 0
	MaxPeers                        int32         = 50
	SyncBlocksBatchSize             uint64        = 100
	MaxcheckpointScanBlocksRange    uint64        = 100
	ReadyGapThreshold               uint64        = 0
	LatestSlotSyncGapThreshold      uint64        = 1
	WaitWorldLatestSlotTimeInterval time.Duration = 50 * time.Millisecond
	InitRequestLatestSlotMaxRetries int           = 3
)
