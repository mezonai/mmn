package p2p

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mezonai/mmn/discovery"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/monitoring"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

func (ln *Libp2pNetwork) RequestNodeInfo(bootstrapPeer string, info *peer.AddrInfo) error {
	ctx := context.Background()

	logx.Info("NETWORK CONNECTED AND REQUEST NODE INFO TO JOIN", bootstrapPeer)

	if len(ln.host.Network().Peers()) < ln.maxPeers {
		if err := ln.host.Connect(ctx, *info); err != nil {
			logx.Error("NETWORK:SETUP", "connect bootstrap", err.Error())
		}
	}

	return nil
}

func (ln *Libp2pNetwork) Discovery(dsc discovery.Discovery, ctx context.Context, h host.Host) {
	func() {
		for {
			peerChan, err := dsc.FindPeers(ctx, AdvertiseName, ln.maxPeers)
			if err != nil {
				logx.Error("DISCOVERY", "Failed to find peers:", err)
				time.Sleep(10 * time.Second)
				continue
			}

			for p := range peerChan {
				if p.ID == h.ID() || len(p.Addrs) == 0 {
					continue
				}

				if len(h.Network().Peers()) >= ln.maxPeers {
					break
				}

				err := h.Connect(ctx, p)
				if err != nil {
					logx.Error("DISCOVERY", "Failed to connect to discovered peer:", err)
				} else {
					logx.Info("DISCOVERY", "Connected to discovered peer:", p.ID.String())
				}
			}

			// Update peer count metric after each discovery cycle
			monitoring.SetPeerCount(ln.GetPeersConnected())
			time.Sleep(30 * time.Second)
		}
	}()
}

func (ln *Libp2pNetwork) RequestLatestSlotFromPeers(ctx context.Context) (uint64, error) {
	logx.Info("NETWORK:LATEST SLOT", "Requesting latest slot from peers")

	// Check connected peers
	peers := ln.host.Network().Peers()
	logx.Info("NETWORK:LATEST SLOT", "Connected peers count:", len(peers))
	for _, peerID := range peers {
		logx.Info("NETWORK:LATEST SLOT", "Connected peer:", peerID.String())
	}

	req := LatestSlotRequest{
		RequesterID: ln.host.ID().String(),
		Addrs:       ln.host.Addrs(),
	}

	data, err := jsonx.Marshal(req)
	if err != nil {
		logx.Error("NETWORK:LATEST SLOT", "Failed to marshal request:", err)
		return 0, err
	}

	if ln.topicLatestSlot == nil {
		errMsg := "latest slot topic is not initialized"
		logx.Error("NETWORK:LATEST SLOT", errMsg)
		return 0, errors.New(errMsg)
	}

	err = ln.topicLatestSlot.Publish(ctx, data)
	if err != nil {
		logx.Error("NETWORK:LATEST SLOT", "Failed to publish request:", err)
		return 0, err
	}
	logx.Info("NETWORK:LATEST SLOT", "Latest slot request published successfully")
	return 0, nil
}

func (ln *Libp2pNetwork) RequestBlockSync(ctx context.Context, fromSlot uint64) error {
	logx.Info("NETWORK:SYNC BLOCK", "Requesting block sync from slot", fromSlot)
	toSlot := fromSlot + SyncBlocksBatchSize - 1

	requestID := GenerateSyncRequestID()
	if requestID == "" {
		return fmt.Errorf("failed to generate sync request ID")
	}

	// new track
	tracker := NewSyncRequestTracker(requestID, fromSlot, toSlot)

	ln.syncTrackerMu.Lock()
	ln.syncRequests[requestID] = tracker
	ln.syncTrackerMu.Unlock()

	req := SyncRequest{
		RequestID: requestID,
		FromSlot:  fromSlot,
		ToSlot:    toSlot,
	}

	data, err := jsonx.Marshal(req)
	if err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "Failed to marshal sync request:", err)
		return err
	}

	if ln.topicBlockSyncReq == nil {
		errMsg := "sync request topic is not initialized"
		return errors.New(errMsg)
	}

	err = ln.topicBlockSyncReq.Publish(ctx, data)
	if err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "Failed to publish sync request for slot", fromSlot, "error", err)
		return err
	}
	return nil
}

func (ln *Libp2pNetwork) RequestSingleBlockSync(ctx context.Context, slot uint64) error {
	requestID := GenerateSyncRequestID()
	if requestID == "" {
		return fmt.Errorf("failed to generate sync request ID")
	}
	tracker := NewSyncRequestTracker(requestID, slot, slot)
	ln.syncTrackerMu.Lock()
	ln.syncRequests[requestID] = tracker
	ln.syncTrackerMu.Unlock()

	req := SyncRequest{
		RequestID: requestID,
		FromSlot:  slot,
		ToSlot:    slot,
	}
	data, err := jsonx.Marshal(req)
	if err != nil {
		return err
	}
	if ln.topicBlockSyncReq == nil {
		return fmt.Errorf("sync request topic is not initialized")
	}
	if err := ln.topicBlockSyncReq.Publish(ctx, data); err != nil {
		return err
	}
	return nil
}

func (ln *Libp2pNetwork) RequestBlockSyncFromLatest(ctx context.Context) error {
	_, err := ln.RequestLatestSlotFromPeers(ctx)
	if err != nil {
		logx.Error("NETWORK:SYNC BLOCK", "Failed to request latest slot from peers:", err)
	}
	localLatestSlot := ln.blockStore.GetLatestFinalizedSlot()
	return ln.RequestBlockSync(ctx, localLatestSlot+1)
}
