package p2p

import (
	"context"
	"crypto/ed25519"
	"sync"
	"time"

	"github.com/mezonai/mmn/mem_blockstore"
	"github.com/mezonai/mmn/store"

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

	blockStore    store.BlockStore
	memBlockStore *mem_blockstore.MemBlockStore

	topicBlocks            *pubsub.Topic
	topicVotes             *pubsub.Topic
	topicCerts             *pubsub.Topic
	topicTxs               *pubsub.Topic
	topicBlockSyncReq      *pubsub.Topic
	topicLatestSlot        *pubsub.Topic
	topicCheckpointRequest *pubsub.Topic

	onBlockReceived        func(broadcastedBlock *block.BroadcastedBlock) error
	onVoteReceived         func(*consensus.Vote) error
	onCertReceived         func(*consensus.Cert) error
	onTransactionReceived  func(*transaction.Transaction) error
	onSyncResponseReceived func([]*block.BroadcastedBlock) error
	onLatestSlotReceived   func(uint64, string) error
	OnSyncPohFromLeader    func(seedHash [32]byte, slot uint64) error

	maxPeers int

	activeSyncRequests map[string]*SyncRequestInfo
	syncMu             sync.RWMutex

	syncRequests  map[string]*SyncRequestTracker
	syncTrackerMu sync.RWMutex

	missingBlocksTracker map[uint64]*MissingBlockInfo
	missingBlocksMu      sync.RWMutex

	lastScannedSlot uint64
	scanMu          sync.RWMutex

	recentlyRequestedSlots map[uint64]time.Time
	recentlyRequestedMu    sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc

	// Add mutex for applyDataToBlock thread safety
	applyBlockMu sync.Mutex
}

type PeerInfo struct {
	ID        peer.ID   `json:"id"`
	PublicKey string    ` json:"public_key"`
	Version   string    `json:"version"`
	LastSeen  time.Time `json:"last_seen"`
	IsActive  bool      `json:"is_active"`
}

type BlockMessage struct {
	Slot      uint64    `json:"slot"`
	PrevHash  string    `json:"prev_hash"`
	Entries   []string  `json:"entries"`
	LeaderID  string    `json:"leader_id"`
	Timestamp time.Time `json:"timestamp"`
	Hash      string    `json:"hash"`
	Signature []byte    ` json:"signature"`
}

type VoteMessage struct {
	Slot      uint64   `json:"slot"`
	VoteType  int      `json:"vote_type"`
	BlockHash [32]byte `json:"block_hash"`
	PubKey    string   `json:"voter_id"`
	Signature []byte   `json:"signature"`
}

type CertMessage struct {
	Slot                 uint64   `json:"slot"`
	CertType             int      `json:"cert_type"`
	BlockHash            [32]byte `json:"block_hash"`
	Stake                uint64   `json:"stake"`
	AggregateSig         []byte   `json:"aggregate_sig"`
	AggregateSigFallback []byte   `json:"aggregate_sig_fallback"`
	ListPubKeys          []string `json:"list_pub_keys"`
	ListPubKeysFallback  []string `json:"list_pub_keys_fallback"`
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

type RepairRequest struct {
	Slot      uint64   `json:"slot"`
	BlockHash [32]byte `json:"block_hash"`
}

type LatestSlotRequest struct {
	RequesterID string                `json:"requester_id"`
	Addrs       []multiaddr.Multiaddr `json:"addrs"`
}

type LatestSlotResponse struct {
	LatestSlot uint64 `json:"latest_slot"`
	PeerID     string `json:"peer_id"`
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
	OnVoteReceived         func(*consensus.Vote) error
	OnCertReceived         func(*consensus.Cert) error
	OnTransactionReceived  func(*transaction.Transaction) error
	OnLatestSlotReceived   func(uint64, string) error
	OnSyncResponseReceived func([]*block.BroadcastedBlock) error
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
