package p2p

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"mmn/block"
	"mmn/blockstore"
	"mmn/config"
	"mmn/consensus"
	"mmn/ledger"
	"mmn/logx"
	"mmn/mempool"
	"mmn/types"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

func (ln *Libp2pNetwork) handleNodeInfoStream(s network.Stream) {
	defer s.Close()

	buf := make([]byte, 2048)
	n, err := s.Read(buf)
	if err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", "Failed to read from bootstrap: ", err)
		return
	}

	var msg map[string]interface{}
	if err := json.Unmarshal(buf[:n], &msg); err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", "Failed to unmarshal peer info: ", err)
		return
	}

	newPeerIDStr := msg["new_peer_id"].(string)
	newPeerID, err := peer.Decode(newPeerIDStr)
	if err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", "Invalid peer ID: ", newPeerIDStr)
		return
	}

	addrStrs := msg["addrs"].([]interface{})
	var addrs []ma.Multiaddr
	for _, a := range addrStrs {
		maddr, err := ma.NewMultiaddr(a.(string))
		if err == nil {
			addrs = append(addrs, maddr)
		}
	}

	peerInfo := peer.AddrInfo{
		ID:    newPeerID,
		Addrs: addrs,
	}

	err = ln.host.Connect(context.Background(), peerInfo)
	if err != nil {
		logx.Error("NETWORK:HANDLE NODE INFOR STREAM", "Failed to connect to new peer: ", err)
	} else {
		logx.Info("NETWORK:HANDLE NODE INFOR STREAM", "Connected to new peer: ", newPeerID)
	}
}

func (ln *Libp2pNetwork) SetupCallbacks(ld *ledger.Ledger, privKey ed25519.PrivateKey, self config.NodeConfig, bs *blockstore.BlockStore, collector *consensus.Collector, mp *mempool.Mempool) {
	// no call back for now
}

func (ln *Libp2pNetwork) SetupPubSubTopics() {
	// no topic for now

}

func (ln *Libp2pNetwork) SetCallbacks() {
	// no call back for now
}

func (ln *Libp2pNetwork) BroadcastToStreams(data []byte, streams map[peer.ID]network.Stream) {
	ln.streamMu.RLock()
	defer ln.streamMu.RUnlock()

	for _, stream := range streams {
		if stream != nil {
			stream.Write(data)
		}
	}
}

func (ln *Libp2pNetwork) TxBroadcast(ctx context.Context, tx *types.Transaction) error {
	// TODO: implement actual broadcasting logic via streams or pubsub

	return nil
}

func (ln *Libp2pNetwork) BroadcastVote(ctx context.Context, vote *consensus.Vote) error {
	// TODO: implement actual broadcasting logic via streams or pubsub

	return nil
}

func (ln *Libp2pNetwork) BroadcastBlock(ctx context.Context, blk *block.Block) error {
	// TODO: implement actual broadcasting logic via streams or pubsub

	return nil
}
