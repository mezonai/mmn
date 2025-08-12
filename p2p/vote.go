package p2p

import (
	"context"
	"encoding/json"
	"mmn/logx"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func (ln *Libp2pNetwork) HandleVoteTopic(sub *pubsub.Subscription) {
	for {
		logx.Info("NETWORK:VOTE", "Received vote topic")
		msg, err := sub.Next(context.Background())
		if err != nil {
			continue
		}

		// Skip messages from self to avoid processing own messages
		if msg.ReceivedFrom == ln.host.ID() {
			logx.Info("NETWORK:VOTE", "Skipping vote message from self")
			continue
		}

		var voteMsg VoteMessage
		if err := json.Unmarshal(msg.Data, &voteMsg); err != nil {
			continue
		}

		vote := ln.ConvertMessageToVote(voteMsg)
		if vote != nil && ln.onVoteReceived != nil {
			ln.onVoteReceived(vote)
		}
	}
}
