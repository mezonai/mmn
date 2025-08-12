package p2p

import (
	"context"
	"encoding/json"
	"mmn/logx"
	"mmn/types"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func (ln *Libp2pNetwork) HandleTxTopic(sub *pubsub.Subscription) {
	for {
		logx.Info("NETWORK:TX", "Received tx topic")
		msg, err := sub.Next(context.Background())
		if err != nil {
			continue
		}

		// Skip messages from self to avoid processing own messages
		if msg.ReceivedFrom == ln.host.ID() {
			logx.Info("NETWORK:TX", "Skipping tx message from self")
			continue
		}

		var tx *types.Transaction
		if err := json.Unmarshal(msg.Data, &tx); err != nil {
			continue
		}

		if tx != nil && ln.onTxReceived != nil {
			ln.onTxReceived(tx)
		}
	}
}
