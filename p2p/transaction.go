package p2p

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/transaction"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func (ln *Libp2pNetwork) HandleTransactionTopic(ctx context.Context, sub *pubsub.Subscription) {
	// Worker pool for parallel message processing
	workerPool := make(chan struct{}, 10) // 10 concurrent workers

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

			// Process message in parallel
			workerPool <- struct{}{}
			go func(message *pubsub.Message) {
				defer func() { <-workerPool }()

				var tx *transaction.Transaction
				if err := json.Unmarshal(message.Data, &tx); err != nil {
					logx.Warn("NETWORK:TX", "Unmarshal error:", err)
					return
				}

				if tx != nil && ln.onTransactionReceived != nil {
					ln.onTransactionReceived(tx)
				}
			}(msg)
		}
	}
}

func (ln *Libp2pNetwork) TxBroadcast(ctx context.Context, tx *transaction.Transaction) error {
	logx.Info("TX", "Broadcasting transaction to network")
	txData, err := json.Marshal(tx)
	if err != nil {
		return fmt.Errorf("failed to serialize transaction: %w", err)
	}

	if err := ln.topicTxs.Publish(ctx, txData); err != nil {
		return fmt.Errorf("failed to publish transaction: %w", err)
	}

	return nil
}
