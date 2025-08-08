package network

import (
	"context"
	"encoding/json"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
)

func (ln *Libp2pNetwork) HandleVoteTopic(sub *pubsub.Subscription) {
	for {
		msg, err := sub.Next(context.Background())
		if err != nil {
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

func (ln *Libp2pNetwork) HandleVoteStream(s network.Stream) {
	defer s.Close()

	buf := make([]byte, 1024)
	n, err := s.Read(buf)
	if err != nil {
		return
	}

	var msg VoteMessage
	if err := json.Unmarshal(buf[:n], &msg); err != nil {
		return
	}

	vote := ln.ConvertMessageToVote(msg)
	if vote != nil && ln.onVoteReceived != nil {
		ln.onVoteReceived(vote)
	}
}
