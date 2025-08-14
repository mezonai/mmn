package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mezonai/mmn/discovery"
	"github.com/mezonai/mmn/logx"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

func (ln *Libp2pNetwork) RequestBlockSync(ctx context.Context, fromSlot uint64) error {
	req := SyncRequest{FromSlot: fromSlot}
	data, err := json.Marshal(req)
	if err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "Failed to marshal sync request:", err)
		return err
	}

	if ln.topicBlockSyncReq == nil {
		errMsg := "sync request topic is not initialized"
		logx.Error("NETWORK:SYNC BLOCK", errMsg)
		return fmt.Errorf(errMsg)
	}

	err = ln.topicBlockSyncReq.Publish(ctx, data)
	if err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "Failed to publish sync request for slot", fromSlot, "error", err)
		return err
	}

	return nil
}

func (ln *Libp2pNetwork) RequestBlockSyncFromLatest(ctx context.Context) error {
	var fromSlot uint64 = 0

	if boundary, ok := ln.blockStore.LastEntryInfoAtSlot(0); ok {
		fromSlot = boundary.Slot + 1
		logx.Info("NETWORK:SYNC BLOCK", "Latest slot in store ", boundary.Slot, ",", " requesting sync from slot ", fromSlot)
	} else {
		logx.Info("NETWORK:SYNC BLOCK", "Block store appears empty, requesting sync from slot 0")
	}

	return ln.RequestBlockSync(ctx, fromSlot)
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
