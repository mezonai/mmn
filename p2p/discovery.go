package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"mmn/discovery"
	"mmn/logx"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

func (ln *Libp2pNetwork) RequestBlockSync(ctx context.Context, fromSlot uint64) error {
	req := SyncRequest{FromSlot: fromSlot}
	data, err := json.Marshal(req)
	if err != nil {
		errMsg := fmt.Sprintf("failed to marshal sync request: %v", err)
		logx.Error("NETWORK:SYNC BLOCK", errMsg)
		return err
	}

	logx.Info("NETWORK:SYNC BLOCK", fmt.Sprintf("Requesting block sync from slot %d", fromSlot))
	return ln.topicBlockSyncReq.Publish(ctx, data)
}
func (ln *Libp2pNetwork) RequestNodeInfo(bootstrapPeer string, info *peer.AddrInfo) error {
	ctx := context.Background()

	logx.Info("NETWORK CONNECTED AND REQYEST NODE INFO TO JOIN", bootstrapPeer)

	if len(ln.host.Network().Peers()) < int(ln.maxPeers) {
		if err := ln.host.Connect(ctx, *info); err != nil {
			logx.Error("NETWORK:SETUP", "connect bootstrap", err.Error())
		}
	}

	return nil
}

func (ln *Libp2pNetwork) Discovery(discovery discovery.Discovery, ctx context.Context, h host.Host) {
	func() {
		for {
			peerChan, err := discovery.FindPeers(ctx, AdvertiseName, int(ln.maxPeers))
			if err != nil {
				logx.Error("DISCOVERY", "Failed to find peers:", err)
				time.Sleep(10 * time.Second)
				continue
			}

			for p := range peerChan {
				if p.ID == h.ID() || len(p.Addrs) == 0 {
					continue
				}

				if len(h.Network().Peers()) >= int(ln.maxPeers) {
					break
				}

				err := h.Connect(ctx, p)
				if err != nil {
					logx.Error("DISCOVERY", "Failed to connect to discovered peer:", err)
				} else {
					logx.Info("DISCOVERY", "Connected to discovered peer:", p.ID.String())
				}
			}

			time.Sleep(30 * time.Second)
		}
	}()
}
