package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"mmn/logx"

	"github.com/libp2p/go-libp2p/core/peer"
)

func (ln *Libp2pNetwork) RequestBlockSync(fromSlot uint64) error {
	req := SyncRequest{FromSlot: fromSlot}
	data, err := json.Marshal(req)
	if err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "failed to marshal sync request: %w", err)
		return err
	}

	logx.Info("NETWORK:SYNC BLOCK", fmt.Sprintf("Requesting block sync from slot %d", fromSlot))
	return ln.topicBlockSyncReq.Publish(context.Background(), data)
}

func (ln *Libp2pNetwork) RequestNodeInfo(bootstrapPeer string, info *peer.AddrInfo) error {
	ctx := context.Background()

	logx.Info("NETWORK CONNECTED AND REQYEST NODE INFO TO JOIN", bootstrapPeer)

	if len(ln.host.Network().Peers()) < ln.maxPeers {
		if err := ln.host.Connect(ctx, *info); err != nil {
			logx.Error("NETWORK:SETUP", "connect bootstrap", err.Error())
		}
	}

	return nil
}
