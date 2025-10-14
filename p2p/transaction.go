package p2p

import (
	"context"
	"fmt"

	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/transaction"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func (ln *Libp2pNetwork) HandleTransactionTopic(ctx context.Context, sub *pubsub.Subscription) {
	for {
		select {
		case <-ctx.Done():
			logx.Info("NETWORK:TX", "Stopping tx topic handler")
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logx.Warn("NETWORK:TX", "Next error:", err)
				continue
			}

			if msg.ReceivedFrom == ln.host.ID() {
				logx.Debug("NETWORK:TX", "Skipping tx message from self")
				continue
			}

			var tx *transaction.Transaction
			if err := jsonx.Unmarshal(msg.Data, &tx); err != nil {
				logx.Warn("NETWORK:TX", "Unmarshal error:", err)
				continue
			}

			if tx != nil && ln.onTransactionReceived != nil {
				// Process transaction and update peer score based on validity
				err := ln.onTransactionReceived(tx)
				if err != nil {
					// If this peer corresponds to the tx sender identity (when sender public key encodings match), skip penalty
					isLeaderSender := ln.peerMatchesID(msg.ReceivedFrom, tx.Sender)
					if !isLeaderSender {
						ln.UpdatePeerScore(msg.ReceivedFrom, "invalid_tx", nil)
					}
					logx.Warn("NETWORK:TX", "Invalid transaction from peer:", msg.ReceivedFrom.String(), "error:", err)
				} else {
					// Valid transaction - reward peer
					ln.UpdatePeerScore(msg.ReceivedFrom, "valid_tx", nil)
				}
			}
		}
	}
}

func (ln *Libp2pNetwork) TxBroadcast(ctx context.Context, tx *transaction.Transaction) error {
	logx.Debug("TX", "Broadcasting transaction to network")
	txData, err := jsonx.Marshal(tx)
	if err != nil {
		return fmt.Errorf("failed to serialize transaction: %w", err)
	}

	if ln.topicTxs != nil {
		if err := ln.topicTxs.Publish(ctx, txData); err != nil {
			return fmt.Errorf("failed to publish transaction: %w", err)
		}
	}

	return nil
}
