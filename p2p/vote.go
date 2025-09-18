package p2p

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mezonai/mmn/consensus"
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

func (ln *Libp2pNetwork) BroadcastVote(ctx context.Context, vote *consensus.Vote) error {
	msg := VoteMessage{
		Slot:      vote.Slot,
		VoteType:  int(vote.VoteType),
		BlockHash: fmt.Sprintf("%x", vote.BlockHash),
		VoterID:   vote.VoterID,
		Signature: vote.Signature,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	if ln.topicVotes != nil {
		ln.topicVotes.Publish(ctx, data)
	}
	return nil
}
