package network

import (
	"context"
	"encoding/json"
	"fmt"
	"mmn/logx"

	"github.com/libp2p/go-libp2p/core/peer"
)

func (ln *Libp2pNetwork) RequestBlockSync(fromSlot uint64) error {
	req := SyncRequest{FromSlot: fromSlot}
	data, _ := json.Marshal(req)
	logx.Info("NETWORK:SYNC BLOCK", fmt.Sprintf("Requesting block sync from slot %d", fromSlot))
	return ln.topicBlockSyncReq.Publish(context.Background(), data)
}

func (ln *Libp2pNetwork) RequestNodeInfo(bootstrapPeer string, info *peer.AddrInfo) error {
	ctx := context.Background()

	logx.Info("NETWORK CONNECTED AND REQYEST NODE INFO TO JOIN", bootstrapPeer)

	if len(ln.host.Network().Peers()) >= ln.maxPeers {
		logx.Info("NETWORK", "Max peers reached, skipping connect to", bootstrapPeer)
		return nil
	}

	if len(ln.host.Network().Peers()) < ln.maxPeers {
		if err := ln.host.Connect(ctx, *info); err != nil {
			logx.Error("NETWORK:SETUP", "connect bootstrap", err.Error())
		}
	}

	stream, err := ln.host.NewStream(context.Background(), info.ID, NodeInfoProtocol)
	if err != nil {
		logx.Error("NETWORK:SETUP", "Failed to open stream:", err)
	}
	defer stream.Close()

	msg := map[string]interface{}{
		"peer_id": ln.host.ID().String(),
	}
	data, _ := json.Marshal(msg)
	stream.Write(data)

	return nil
}
