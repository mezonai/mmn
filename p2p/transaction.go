package p2p

import (
	"context"
	"fmt"

	"github.com/mezonai/mmn/logx"
	pb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/utils"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"google.golang.org/protobuf/proto"
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

			signed := &pb.SignedTxMsg{}
			if err := proto.Unmarshal(msg.Data, signed); err != nil {
				logx.Warn("NETWORK:TX", "Unmarshal error:", err)
				continue
			}
			tx, err := utils.FromProtoSignedTx(signed)
			if err != nil {
				logx.Warn("NETWORK:TX", "Convert error:", err)
				continue
			}
			if tx != nil && ln.onTransactionReceived != nil {
				ln.onTransactionReceived(tx)
			}
		}
	}
}

func (ln *Libp2pNetwork) TxBroadcast(ctx context.Context, tx *transaction.Transaction) error {
	logx.Info("TX", "Broadcasting transaction to network")
	pbSigned := utils.ToProtoSignedTx(tx)
	txData, err := proto.Marshal(pbSigned)
	if err != nil {
		return fmt.Errorf("failed to serialize transaction: %w", err)
	}

	if err := ln.topicTxs.Publish(ctx, txData); err != nil {
		return fmt.Errorf("failed to publish transaction: %w", err)
	}

	return nil
}
