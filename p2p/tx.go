package p2p

import (
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
)

func (ln *Libp2pNetwork) HandleTxTopic(sub *pubsub.Subscription) {

	// TODO: implement handle TX in topic channel
}

func (ln *Libp2pNetwork) HandleTxStream(s network.Stream) {
	// TODO: implement handle TX in topic p2p

}
