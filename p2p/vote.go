package p2p

import (
	"context"
	"fmt"

	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/ledger"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/mempool"
	"github.com/mezonai/mmn/store"

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

			var voteMsg VoteMessage
			if err := jsonx.Unmarshal(msg.Data, &voteMsg); err != nil {
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
		BlockHash: fmt.Sprintf("%x", vote.BlockHash),
		VoterID:   vote.VoterID,
		Signature: vote.Signature,
	}

	data, err := jsonx.Marshal(msg)
	if err != nil {
		return err
	}

	if ln.topicVotes != nil {
		ln.topicVotes.Publish(ctx, data)
	}
	return nil
}

func (ln *Libp2pNetwork) ProcessVote(bs store.BlockStore, ld *ledger.Ledger, mp *mempool.Mempool, vote *consensus.Vote, collector *consensus.Collector) error {
	committed, needApply, err := collector.AddVote(vote)
	if err != nil {
		logx.Error("VOTE", "Failed to add vote: ", err)
		return err
	}

	if existed := bs.HasCompleteBlock(vote.Slot); !existed {
		logx.Info("VOTE", "Received vote from network: slot= ", vote.Slot, ",voter= ", vote.VoterID, " but dont have block")
		return nil
	}

	if committed && needApply {
		logx.Info("VOTE", "Committed vote from OnVote Received: slot= ", vote.Slot, ",voter= ", vote.VoterID)
		err := ln.applyDataToBlock(vote, bs)
		if err != nil {
			logx.Error("VOTE", "Failed to apply data to block: ", err)
			return err
		}
	}
	return nil
}
