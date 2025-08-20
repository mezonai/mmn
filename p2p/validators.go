package p2p

import (
	"context"
	"encoding/json"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
)

func (ln *Libp2pNetwork) registerTopicValidators() error {
	// Basic JSON parsing validators; heavy verification continues in handlers
	if err := ln.pubsub.RegisterTopicValidator(TopicBlocks, ln.blocksValidator, pubsub.WithValidatorInline(true)); err != nil {
		return err
	}
	if err := ln.pubsub.RegisterTopicValidator(TopicVotes, ln.votesValidator, pubsub.WithValidatorInline(true)); err != nil {
		return err
	}
	if err := ln.pubsub.RegisterTopicValidator(TopicTxs, ln.txsValidator, pubsub.WithValidatorInline(true)); err != nil {
		return err
	}
	return nil
}

func (ln *Libp2pNetwork) blocksValidator(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
	var tmp map[string]interface{}
	if err := json.Unmarshal(msg.Data, &tmp); err != nil {
		ln.UpdatePeerScore(p, "invalid_block", nil)
		return pubsub.ValidationReject
	}
	return pubsub.ValidationAccept
}

func (ln *Libp2pNetwork) votesValidator(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
	var tmp map[string]interface{}
	if err := json.Unmarshal(msg.Data, &tmp); err != nil {
		ln.UpdatePeerScore(p, "invalid_tx", nil)
		return pubsub.ValidationReject
	}
	return pubsub.ValidationAccept
}

func (ln *Libp2pNetwork) txsValidator(ctx context.Context, p peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
	var tmp map[string]interface{}
	if err := json.Unmarshal(msg.Data, &tmp); err != nil {
		ln.UpdatePeerScore(p, "invalid_tx", nil)
		return pubsub.ValidationReject
	}
	return pubsub.ValidationAccept
}
