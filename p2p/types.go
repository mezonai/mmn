package p2p

import (
	"context"
	"crypto/ed25519"
	"sync"
	"time"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/blockstore"
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
	mu          sync.RWMutex

	blockStore blockstore.Store

	topicBlocks            *pubsub.Topic
	topicVotes             *pubsub.Topic
	topicTxs               *pubsub.Topic
	topicBlockSyncReq      *pubsub.Topic
	topicLatestSlot        *pubsub.Topic
	topicAccessControl     *pubsub.Topic
	topicAccessControlSync *pubsub.Topic

	onBlockReceived        func(broadcastedBlock *block.BroadcastedBlock) error
	onVoteReceived         func(*consensus.Vote) error
	onTransactionReceived  func(*transaction.Transaction) error
	onSyncResponseReceived func([]*block.BroadcastedBlock) error
	onLatestSlotReceived   func(uint64, string) error

	syncStreams map[peer.ID]network.Stream
	maxPeers    int

	activeSyncRequests map[string]*SyncRequestInfo
	syncMu             sync.RWMutex

	syncRequests  map[string]*SyncRequestTracker
	syncTrackerMu sync.RWMutex

	// Authentication tracking
	authenticatedPeers map[peer.ID]*AuthenticatedPeer
	authMu             sync.RWMutex
	pendingChallenges  map[peer.ID][]byte // peer.ID -> challenge bytes
	challengeMu        sync.RWMutex

	// Access control - allowlist and blacklist
	allowlist        map[peer.ID]bool
	blacklist        map[peer.ID]bool
	allowlistEnabled bool
	blacklistEnabled bool
	listMu           sync.RWMutex

	// Peer scoring system
	peerScoringManager *PeerScoringManager

	ctx    context.Context
	cancel context.CancelFunc
}

type PeerInfo struct {
	ID              peer.ID   `json:"id"`
	PublicKey       string    `json:"public_key"`
	Version         string    `json:"version"`
	LastSeen        time.Time `json:"last_seen"`
	IsActive        bool      `json:"is_active"`
	IsAuthenticated bool      `json:"is_authenticated"`
	AuthTimestamp   time.Time `json:"auth_timestamp"`
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
	Slot      uint64 `json:"slot"`
	BlockHash string `json:"block_hash"`
	VoterID   string `json:"voter_id"`
	Signature []byte `json:"signature"`
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
	mu           sync.RWMutex
}

type Callbacks struct {
	OnBlockReceived        func(broadcastedBlock *block.BroadcastedBlock) error
	OnVoteReceived         func(*consensus.Vote) error
	OnTransactionReceived  func(*transaction.Transaction) error
	OnLatestSlotReceived   func(uint64, string) error
	OnSyncResponseReceived func([]*block.BroadcastedBlock) error
}

type AuthChallenge struct {
	Version   string `json:"version"`
	Challenge []byte `json:"challenge"`
	PeerID    string `json:"peer_id"`
	PublicKey string `json:"public_key"`
	Timestamp int64  `json:"timestamp"`
	Nonce     uint64 `json:"nonce,string"`
	ChainID   string `json:"chain_id"`
}

type AuthResponse struct {
	Version   string `json:"version"`
	Challenge []byte `json:"challenge"`
	Signature []byte `json:"signature"`
	PeerID    string `json:"peer_id"`
	PublicKey string `json:"public_key"`
	Timestamp int64  `json:"timestamp"`
	Nonce     uint64 `json:"nonce,string"`
	ChainID   string `json:"chain_id"`
}

type AuthResult struct {
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
	Timestamp int64  `json:"timestamp"`
	PeerID    string `json:"peer_id,omitempty"`
}

const (
	AuthVersion    = "1.0.0"
	DefaultChainID = "mmn"
)

type AuthenticatedPeer struct {
	PeerID        peer.ID
	PublicKey     ed25519.PublicKey
	AuthTimestamp time.Time
	IsValid       bool
}

type AccessControlUpdate struct {
	Type      string `json:"type"`
	Action    string `json:"action"`
	PeerID    string `json:"peer_id"`
	Timestamp int64  `json:"timestamp"`
	NodeID    string `json:"node_id"`
}

type AccessControlSync struct {
	Type       string   `json:"type"`
	PeerIDs    []string `json:"peer_ids"`
	Timestamp  int64    `json:"timestamp"`
	NodeID     string   `json:"node_id"`
	IsFullSync bool     `json:"is_full_sync"`
}
