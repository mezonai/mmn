package p2p

import (
	"context"
	"encoding/json"
	"mmn/logx"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func (ln *Libp2pNetwork) HandleVoteTopic(ctx context.Context, sub *pubsub.Subscription) {
	for {
		select {
		case <-ctx.Done():
			logx.Info("NETWORK:VOTE", "Stopping vote topic handler")
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logx.Warn("NETWORK:VOTE", "Next error:", err)
				continue
			}
			if msg.ReceivedFrom == ln.host.ID() {
				logx.Debug("NETWORK:VOTE", "Skipping vote message from self")
				continue
			}

			var voteMsg VoteMessage
			if err := json.Unmarshal(msg.Data, &voteMsg); err != nil {
				logx.Warn("NETWORK:VOTE", "Unmarshal error:", err)
				continue
			}

			vote := ln.ConvertMessageToVote(voteMsg)
			if vote != nil && ln.onVoteReceived != nil {
				ln.onVoteReceived(vote)
			}
		}
	}
}
