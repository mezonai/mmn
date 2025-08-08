package network

import (
	"context"
	"encoding/json"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
)

func (ln *Libp2pNetwork) HandleTxTopic(sub *pubsub.Subscription) {
	for {
		msg, err := sub.Next(context.Background())
		if err != nil {
			continue
		}

		var txMsg TxMessage
		if err := json.Unmarshal(msg.Data, &txMsg); err != nil {
			continue
		}

		if ln.onTxReceived != nil {
			ln.onTxReceived(txMsg.Data)
		}
	}
}

func (ln *Libp2pNetwork) HandleTxStream(s network.Stream) {
	defer s.Close()

	buf := make([]byte, 1024)
	n, err := s.Read(buf)
	if err != nil {
		return
	}

	var msg TxMessage
	if err := json.Unmarshal(buf[:n], &msg); err != nil {
		return
	}

	if ln.onTxReceived != nil {
		ln.onTxReceived(msg.Data)
	}
}
