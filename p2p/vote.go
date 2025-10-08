package p2p

import (
	"context"

	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"

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
				continue
			}

			var vote *consensus.Vote
			if err := jsonx.Unmarshal(msg.Data, &vote); err != nil {
				logx.Warn("NETWORK:VOTE", "Unmarshal error:", err)
				continue
			}

			if ln.onVoteReceived != nil {
				ln.onVoteReceived(vote)
			}
		}
	}
}

func (ln *Libp2pNetwork) BroadcastVote(ctx context.Context, vote *consensus.Vote) error {
	data, err := jsonx.Marshal(vote)
	if err != nil {
		return err
	}

	if ln.topicVotes != nil {
		ln.topicVotes.Publish(ctx, data)
	}
	return nil
}
