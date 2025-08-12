package p2p

import (
	"context"
	"encoding/json"
	"mmn/logx"
	"mmn/types"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
)

func (ln *Libp2pNetwork) HandleTxTopic(sub *pubsub.Subscription) {
	for {
		msg, err := sub.Next(context.Background())
		if err != nil {
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

func (ln *Libp2pNetwork) HandleTxStream(s network.Stream) {
	logx.Info("NETWORK:TX", "Received tx stream")
	defer s.Close()

	buf := make([]byte, 4096)
	n, err := s.Read(buf)
	if err != nil {
		return
	}

	var tx *types.Transaction
	if err := json.Unmarshal(buf[:n], &tx); err != nil {
		return
	}

	if tx != nil && ln.onTxReceived != nil {
		ln.onTxReceived(tx)
	}
}
