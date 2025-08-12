package p2p

import (
	"crypto/ed25519"
	"mmn/block"
	"mmn/blockstore"
	"mmn/consensus"
	"mmn/types"
	"sync"
	"time"

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

	blockStore *blockstore.BlockStore

	topicBlocks       *pubsub.Topic
	topicVotes        *pubsub.Topic
	topicTxs          *pubsub.Topic
	topicBlockSyncReq *pubsub.Topic
	topicBlockSyncRes *pubsub.Topic

	onBlockReceived func(*block.Block) error
	onVoteReceived  func(*consensus.Vote) error
	onTxReceived    func(*types.Transaction) error

	blockStreams map[peer.ID]network.Stream
	voteStreams  map[peer.ID]network.Stream
	txStreams    map[peer.ID]network.Stream
	streamMu     sync.RWMutex
	maxPeers     int
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
	Slot      uint64 `json:"slot"`
	BlockHash string `json:"block_hash"`
	VoterID   string `json:"voter_id"`
	Signature []byte `json:"signature"`
}

type TxMessage struct {
	Data []byte `json:"data"`
}

type SyncRequest struct {
	FromSlot uint64 `json:"from_slot"`
}

type SyncResponse struct {
	Blocks []*block.Block `json:"blocks"`
}
