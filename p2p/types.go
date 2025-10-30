package p2p

import (
	"context"
	"crypto/ed25519"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/faucet"
	"github.com/mezonai/mmn/poh"
	"github.com/mezonai/mmn/store"
	"github.com/mezonai/mmn/zkverify"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/transaction"
	"github.com/multiformats/go-multiaddr"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

type Libp2pNetwork struct {
	host        host.Host
	pubsub      *pubsub.PubSub
	selfPubKey  string
	selfPrivKey ed25519.PrivateKey
	peers       map[peer.ID]*PeerInfo
	// Track bootstrap peers so we can exclude them from certain requests
	bootstrapPeerIDs map[peer.ID]struct{}

	blockStore store.BlockStore
	txStore    store.TxStore

	topicBlocks            *pubsub.Topic
	topicEmptyBlocks       *pubsub.Topic
	topicVotes             *pubsub.Topic
	topicTxs               *pubsub.Topic
	topicBlockSyncReq      *pubsub.Topic
	topicLatestSlot        *pubsub.Topic
	topicCheckpointRequest *pubsub.Topic

	topicFaucetMultisigTx       *pubsub.Topic
	topicFaucetConfig           *pubsub.Topic
	topicFaucetWhitelist        *pubsub.Topic
	topicFaucetVote             *pubsub.Topic
	topicRequestFaucetVote      *pubsub.Topic
	topicRequesInitFaucetConfig *pubsub.Topic
	topicInitFaucetConfig       *pubsub.Topic

	onBlockReceived        func(broadcastedBlock *block.BroadcastedBlock) error
	onEmptyBlockReceived   func(blocks []*block.BroadcastedBlock) error
	onVoteReceived         func(*consensus.Vote) error
	onTransactionReceived  func(*transaction.Transaction) error
	onSyncResponseReceived func(*block.BroadcastedBlock) error
	onLatestSlotReceived   func(uint64, uint64, string) error
	OnSyncPohFromLeader    func(seedHash [32]byte, slot uint64) error
	OnForceResetPOH        func(seedHash [32]byte, slot uint64)
	OnGetLatestPohSlot     func() uint64
	OnFaucetVote           func(txHash string, voterID peer.ID, approve bool)

	maxPeers int

	syncRequests  map[string]*SyncRequestTracker
	syncTrackerMu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc

	// Add mutex for applyDataToBlock thread safety
	applyBlockMu sync.Mutex

	// cached leader schedule for local leader checks
	leaderSchedule *poh.LeaderSchedule

	// readiness control
	enableFullModeOnce sync.Once
	ready              atomic.Bool

	// New field for join behavior control
	worldLatestSlot    uint64
	worldLatestPohSlot uint64
	// Global block ordering queue
	blockOrderingQueue map[uint64]*block.BroadcastedBlock
	nextExpectedSlot   uint64
	blockOrderingMu    sync.RWMutex

	OnStartPoh          func()
	OnStartValidator    func()
	OnStartLoadTxHashes func()

	// PoH config
	pohCfg     *config.PohConfig
	isListener bool

	// ZK verify for transaction verification
	zkVerify *zkverify.ZkVerify
	// Multisig faucet store
	multisigStore          store.MultisigFaucetStore
	HandleFaucetWhitelist  func(msg *faucet.FaucetSyncWhitelistMessage) error
	HandleFaucetConfig     func(msg *faucet.FaucetSyncConfigMessage) error
	HandleFaucetMultisigTx func(msg *faucet.FaucetSyncTransactionMessage) error
	VerifyVote             func(msg *faucet.RequestFaucetVoteMessage) bool
	GetFaucetConfig        func() *faucet.RequestInitFaucetConfigMessage
	HandleInitFaucetConfig func(msg *faucet.RequestInitFaucetConfigMessage) error
}

type PeerInfo struct {
	ID        peer.ID   `json:"id"`
	PublicKey string    ` json:"public_key"`
	Version   string    `json:"version"`
	LastSeen  time.Time `json:"last_seen"`
	IsActive  bool      `json:"is_active"`
}

type TxMessage struct {
	Data []byte `json:"data"`
}

type SyncRequest struct {
	RequestID string                `json:"request_id"`
	FromSlot  uint64                `json:"from_slot"`
	ToSlot    uint64                `json:"to_slot"`
	Addrs     []multiaddr.Multiaddr `json:"addrs"`
}

type SyncResponse struct {
	Blocks []*block.Block `json:"blocks"`
}

type LatestSlotRequest struct {
	RequesterID string                `json:"requester_id"`
	Addrs       []multiaddr.Multiaddr `json:"addrs"`
}

type LatestSlotResponse struct {
	LatestSlot    uint64 `json:"latest_slot"`
	PeerID        string `json:"peer_id"`
	LatestPohSlot uint64 `json:"latest_poh_slot"`
}

type SnapshotSyncRequest struct {
	RequesterID string `json:"requester_id"`
	RequestTime int64  `json:"request_time"`
	Slot        uint64 `json:"slot"`
}

type SyncRequestInfo struct {
	RequestID string
	FromSlot  uint64
	ToSlot    uint64
	PeerID    peer.ID
	Stream    network.Stream
	StartTime time.Time
	IsActive  bool
}

// for trach when multiples requests
type SyncRequestTracker struct {
	RequestID    string
	FromSlot     uint64
	ToSlot       uint64
	ActivePeer   peer.ID
	ActiveStream network.Stream
	IsActive     bool
	StartTime    time.Time
	AllPeers     map[peer.ID]network.Stream
	mutex        sync.RWMutex
}

type Callbacks struct {
	OnBlockReceived        func(broadcastedBlock *block.BroadcastedBlock) error
	OnEmptyBlockReceived   func(blocks []*block.BroadcastedBlock) error
	OnVoteReceived         func(*consensus.Vote) error
	OnTransactionReceived  func(*transaction.Transaction) error
	OnLatestSlotReceived   func(uint64, uint64, string) error
	OnSyncResponseReceived func(*block.BroadcastedBlock) error
}

type CheckpointHashRequest struct {
	RequesterID string                `json:"requester_id"`
	Checkpoint  uint64                `json:"checkpoint"`
	Addrs       []multiaddr.Multiaddr `json:"addrs"`
}

type CheckpointHashResponse struct {
	Checkpoint uint64   `json:"checkpoint"`
	Slot       uint64   `json:"slot"`
	BlockHash  [32]byte `json:"block_hash"`
	PeerID     string   `json:"peer_id"`
}

type MissingBlockInfo struct {
	Slot       uint64    `json:"slot"`
	FirstSeen  time.Time `json:"first_seen"`
	LastRetry  time.Time `json:"last_retry"`
	RetryCount int       `json:"retry_count"`
	MaxRetries int       `json:"max_retries"`
}

// Snapshot gossip messages
type SnapshotAnnounce struct {
	Slot      uint64 `json:"slot"`
	BankHash  string `json:"bank_hash"`
	Size      int64  `json:"size"`
	UDPAddr   string `json:"udp_addr"`
	ChunkSize int    `json:"chunk_size"`
	CreatedAt int64  `json:"created_at"`
	PeerID    string `json:"peer_id"`
}

type SnapshotRequest struct {
}

type FaucetVoteMessage struct {
	TxHash  string  `json:"tx_hash"`
	VoterID peer.ID `json:"voter_id"`
	Approve bool    `json:"approve"`
}
