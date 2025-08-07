package p2p

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

const (
	// Protocol IDs
	BlockProtocol = "/mmn/block/1.0.0"
	VoteProtocol  = "/mmn/vote/1.0.0"
	TxProtocol    = "/mmn/tx/1.0.0"

	// PubSub Topics
	BlockTopic = "mmn-blocks"
	VoteTopic  = "mmn-votes"
	TxTopic    = "mmn-transactions"
)

// P2PService handles all peer-to-peer networking for MMN
type P2PService struct {
	host   host.Host
	dht    *dht.IpfsDHT
	pubsub *pubsub.PubSub
	ctx    context.Context
	cancel context.CancelFunc

	// Message handlers
	blockHandler MessageHandler
	voteHandler  MessageHandler
	txHandler    MessageHandler

	// Subscriptions
	blockSub *pubsub.Subscription
	voteSub  *pubsub.Subscription
	txSub    *pubsub.Subscription

	// Topics (for publishing)
	blockTopic *pubsub.Topic
	voteTopic  *pubsub.Topic
	txTopic    *pubsub.Topic

	// Peer management
	peers      map[peer.ID]*PeerInfo
	peersMutex sync.RWMutex

	// Bootstrap nodes
	bootstrapPeers []multiaddr.Multiaddr
}

// MessageHandler defines interface for handling different message types
type MessageHandler interface {
	HandleMessage(ctx context.Context, from peer.ID, data []byte) error
}

// PeerInfo stores information about connected peers
type PeerInfo struct {
	ID          peer.ID
	Addrs       []multiaddr.Multiaddr
	LastSeen    time.Time
	IsValidator bool
}

// Config holds configuration for P2P service
type Config struct {
	ListenAddrs    []string
	BootstrapPeers []string
	PrivateKey     crypto.PrivKey // Optional, will generate if nil
	EnableDHT      bool
	EnablePubSub   bool
}

// NewP2PService creates a new P2P service instance
func NewP2PService(cfg *Config) (*P2PService, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Generate or use provided private key
	var priv crypto.PrivKey
	var err error
	if cfg.PrivateKey != nil {
		priv = cfg.PrivateKey
	} else {
		priv, _, err = crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to generate key pair: %w", err)
		}
	}

	// Create libp2p host with default configuration
	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(cfg.ListenAddrs...),
		libp2p.DefaultSecurity,
		libp2p.DefaultTransports,
		libp2p.DefaultMuxers,
		libp2p.NATPortMap(),
		// Disable AutoRelay for now to avoid configuration issues
		// libp2p.EnableAutoRelay(),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	service := &P2PService{
		host:   h,
		ctx:    ctx,
		cancel: cancel,
		peers:  make(map[peer.ID]*PeerInfo),
	}

	// Parse bootstrap peers
	if len(cfg.BootstrapPeers) > 0 {
		for _, addrStr := range cfg.BootstrapPeers {
			addr, err := multiaddr.NewMultiaddr(addrStr)
			if err != nil {
				log.Printf("Invalid bootstrap address %s: %v", addrStr, err)
				continue
			}
			service.bootstrapPeers = append(service.bootstrapPeers, addr)
		}
	}

	// Initialize DHT if enabled
	if cfg.EnableDHT {
		if err := service.setupDHT(); err != nil {
			service.Close()
			return nil, fmt.Errorf("failed to setup DHT: %w", err)
		}
	}

	// Initialize PubSub if enabled
	if cfg.EnablePubSub {
		if err := service.setupPubSub(); err != nil {
			service.Close()
			return nil, fmt.Errorf("failed to setup PubSub: %w", err)
		}
	}

	// Setup stream handlers
	service.setupStreamHandlers()

	return service, nil
}

// setupDHT initializes the DHT for peer discovery
func (p *P2PService) setupDHT() error {
	var err error
	p.dht, err = dht.New(p.ctx, p.host)
	if err != nil {
		return err
	}

	// Bootstrap the DHT
	if err = p.dht.Bootstrap(p.ctx); err != nil {
		return err
	}

	return nil
}

// setupPubSub initializes the gossip pubsub system
func (p *P2PService) setupPubSub() error {
	var err error
	p.pubsub, err = pubsub.NewGossipSub(p.ctx, p.host)
	if err != nil {
		return err
	}

	return nil
}

// setupStreamHandlers configures protocol stream handlers
func (p *P2PService) setupStreamHandlers() {
	p.host.SetStreamHandler(protocol.ID(BlockProtocol), p.handleBlockStream)
	p.host.SetStreamHandler(protocol.ID(VoteProtocol), p.handleVoteStream)
	p.host.SetStreamHandler(protocol.ID(TxProtocol), p.handleTxStream)
}

// Start begins the P2P service
func (p *P2PService) Start() error {
	log.Printf("P2P service starting on: %v", p.host.Addrs())
	log.Printf("Peer ID: %s", p.host.ID().String())

	// Connect to bootstrap peers
	if err := p.connectToBootstrapPeers(); err != nil {
		log.Printf("Warning: Failed to connect to some bootstrap peers: %v", err)
	}

	// Subscribe to topics if pubsub is enabled
	if p.pubsub != nil {
		if err := p.subscribeToTopics(); err != nil {
			return fmt.Errorf("failed to subscribe to topics: %w", err)
		}
	}

	// Start peer discovery if DHT is enabled
	if p.dht != nil {
		go p.startPeerDiscovery()
	}

	// Start peer management
	go p.managePeers()

	return nil
}

// connectToBootstrapPeers connects to configured bootstrap peers
func (p *P2PService) connectToBootstrapPeers() error {
	if len(p.bootstrapPeers) == 0 {
		return nil
	}

	log.Printf("Connecting to %d bootstrap peers...", len(p.bootstrapPeers))

	for _, addr := range p.bootstrapPeers {
		go func(addr multiaddr.Multiaddr) {
			addrInfo, err := peer.AddrInfoFromP2pAddr(addr)
			if err != nil {
				log.Printf("Invalid bootstrap peer address %s: %v", addr, err)
				return
			}

			ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
			defer cancel()

			if err := p.host.Connect(ctx, *addrInfo); err != nil {
				log.Printf("Failed to connect to bootstrap peer %s: %v", addrInfo.ID, err)
			} else {
				log.Printf("Connected to bootstrap peer: %s", addrInfo.ID)
				p.addPeer(addrInfo.ID, addrInfo.Addrs)
			}
		}(addr)
	}

	return nil
}

// subscribeToTopics subscribes to all required pubsub topics
func (p *P2PService) subscribeToTopics() error {
	var err error

	// Join and subscribe to block topic
	p.blockTopic, err = p.pubsub.Join(BlockTopic)
	if err != nil {
		return fmt.Errorf("failed to join block topic: %w", err)
	}
	p.blockSub, err = p.blockTopic.Subscribe()
	if err != nil {
		return fmt.Errorf("failed to subscribe to block topic: %w", err)
	}
	go p.handlePubSubMessages(p.blockSub, p.blockHandler)

	// Join and subscribe to vote topic
	p.voteTopic, err = p.pubsub.Join(VoteTopic)
	if err != nil {
		return fmt.Errorf("failed to join vote topic: %w", err)
	}
	p.voteSub, err = p.voteTopic.Subscribe()
	if err != nil {
		return fmt.Errorf("failed to subscribe to vote topic: %w", err)
	}
	go p.handlePubSubMessages(p.voteSub, p.voteHandler)

	// Join and subscribe to tx topic
	p.txTopic, err = p.pubsub.Join(TxTopic)
	if err != nil {
		return fmt.Errorf("failed to join tx topic: %w", err)
	}
	p.txSub, err = p.txTopic.Subscribe()
	if err != nil {
		return fmt.Errorf("failed to subscribe to tx topic: %w", err)
	}
	go p.handlePubSubMessages(p.txSub, p.txHandler)

	log.Printf("Subscribed to all pubsub topics")
	return nil
}

// handlePubSubMessages handles incoming pubsub messages
func (p *P2PService) handlePubSubMessages(sub *pubsub.Subscription, handler MessageHandler) {
	for {
		msg, err := sub.Next(p.ctx)
		if err != nil {
			if p.ctx.Err() != nil {
				return // Context cancelled
			}
			log.Printf("Error reading from subscription: %v", err)
			continue
		}

		// Skip messages from ourselves
		if msg.ReceivedFrom == p.host.ID() {
			continue
		}

		if handler != nil {
			if err := handler.HandleMessage(p.ctx, msg.ReceivedFrom, msg.Data); err != nil {
				log.Printf("Error handling pubsub message from %s: %v", msg.ReceivedFrom, err)
			}
		}
	}
}

// startPeerDiscovery starts the peer discovery process using DHT
func (p *P2PService) startPeerDiscovery() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.discoverPeers()
		}
	}
}

// discoverPeers discovers new peers using DHT
func (p *P2PService) discoverPeers() {
	log.Printf("Discovering peers...")

	// Use DHT routing table to find peers
	peers := p.dht.RoutingTable().ListPeers()

	found := 0
	for _, peerID := range peers {
		if peerID == p.host.ID() {
			continue
		}

		// Check if already connected
		if p.host.Network().Connectedness(peerID) == network.Connected {
			continue
		}

		// Try to connect
		ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
		peerInfo := peer.AddrInfo{ID: peerID}
		err := p.host.Connect(ctx, peerInfo)
		cancel()

		if err != nil {
			log.Printf("Failed to connect to discovered peer %s: %v", peerID, err)
		} else {
			log.Printf("Connected to discovered peer: %s", peerID)
			p.addPeer(peerID, peerInfo.Addrs)
			found++
		}
	}

	if found > 0 {
		log.Printf("Discovered and connected to %d new peers", found)
	}
}

// managePeers manages peer connections and cleanup
func (p *P2PService) managePeers() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.cleanupPeers()
		}
	}
}

// cleanupPeers removes stale peer connections
func (p *P2PService) cleanupPeers() {
	p.peersMutex.Lock()
	defer p.peersMutex.Unlock()

	now := time.Now()
	for id, info := range p.peers {
		if now.Sub(info.LastSeen) > 5*time.Minute {
			delete(p.peers, id)
			log.Printf("Removed stale peer: %s", id)
		}
	}
}

// addPeer adds a peer to the peer list
func (p *P2PService) addPeer(id peer.ID, addrs []multiaddr.Multiaddr) {
	p.peersMutex.Lock()
	defer p.peersMutex.Unlock()

	p.peers[id] = &PeerInfo{
		ID:       id,
		Addrs:    addrs,
		LastSeen: time.Now(),
	}
}

// GetPeers returns list of connected peers
func (p *P2PService) GetPeers() []peer.ID {
	p.peersMutex.RLock()
	defer p.peersMutex.RUnlock()

	peers := make([]peer.ID, 0, len(p.peers))
	for id := range p.peers {
		peers = append(peers, id)
	}
	return peers
}

// GetPeerCount returns the number of connected peers
func (p *P2PService) GetPeerCount() int {
	p.peersMutex.RLock()
	defer p.peersMutex.RUnlock()
	return len(p.peers)
}

// Close shuts down the P2P service
func (p *P2PService) Close() error {
	log.Printf("Shutting down P2P service...")

	p.cancel()

	// Close subscriptions
	if p.blockSub != nil {
		p.blockSub.Cancel()
	}
	if p.voteSub != nil {
		p.voteSub.Cancel()
	}
	if p.txSub != nil {
		p.txSub.Cancel()
	}

	// Close topics
	if p.blockTopic != nil {
		p.blockTopic.Close()
	}
	if p.voteTopic != nil {
		p.voteTopic.Close()
	}
	if p.txTopic != nil {
		p.txTopic.Close()
	}

	if p.dht != nil {
		p.dht.Close()
	}

	if p.host != nil {
		return p.host.Close()
	}

	return nil
}

// SetMessageHandlers sets the message handlers for different protocols
func (p *P2PService) SetMessageHandlers(blockHandler, voteHandler, txHandler MessageHandler) {
	p.blockHandler = blockHandler
	p.voteHandler = voteHandler
	p.txHandler = txHandler
}

// Stream handlers for direct peer communication
func (p *P2PService) handleBlockStream(s network.Stream) {
	p.handleStream(s, p.blockHandler)
}

func (p *P2PService) handleVoteStream(s network.Stream) {
	p.handleStream(s, p.voteHandler)
}

func (p *P2PService) handleTxStream(s network.Stream) {
	p.handleStream(s, p.txHandler)
}

func (p *P2PService) handleStream(s network.Stream, handler MessageHandler) {
	defer s.Close()

	data, err := io.ReadAll(s)
	if err != nil {
		log.Printf("Error reading from stream: %v", err)
		return
	}

	if handler != nil {
		if err := handler.HandleMessage(p.ctx, s.Conn().RemotePeer(), data); err != nil {
			log.Printf("Error handling stream message: %v", err)
		}
	}
}
