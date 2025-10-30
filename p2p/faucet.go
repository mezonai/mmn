package p2p

import (
	"context"
	"crypto/ed25519"
	"fmt"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/mezonai/mmn/common"
	"github.com/mezonai/mmn/faucet"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
	"github.com/pkg/errors"
)

func (ln *Libp2pNetwork) HandleFaucetMultisigTxTopic(ctx context.Context, sub *pubsub.Subscription) {
	logx.Info("NETWORK:FAUCET", "Starting faucet multisig tx topic handler")
	for {
		select {
		case <-ctx.Done():
			logx.Info("NETWORK:FAUCET", "Stopping faucet multisig tx topic handler")
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				logx.Error("NETWORK:FAUCET", "Error reading faucet multisig tx message", err)
				continue
			}

			if msg.ReceivedFrom == ln.host.ID() {
				continue
			}

			var tx faucet.FaucetSyncTransactionMessage
			if err := jsonx.Unmarshal(msg.Data, &tx); err != nil {
				logx.Error("NETWORK:FAUCET", "Failed to unmarshal faucet multisig tx message:", err)
				continue
			}
			ln.HandleFaucetMultisigTx(&tx)
		}
	}
}

func (ln *Libp2pNetwork) HandleFaucetConfigTopic(ctx context.Context, sub *pubsub.Subscription) {
	logx.Info("NETWORK:FAUCET", "Starting faucet config topic handler")

	for {
		select {
		case <-ctx.Done():
			logx.Info("NETWORK:FAUCET", "Stopping faucet config topic handler")
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				logx.Error("NETWORK:FAUCET", "Error reading faucet config message", err)
				continue
			}

			if msg.ReceivedFrom == ln.host.ID() {
				continue
			}

			var configMsg faucet.FaucetSyncConfigMessage
			if err := jsonx.Unmarshal(msg.Data, &configMsg); err != nil {
				logx.Error("NETWORK:FAUCET", "Failed to unmarshal faucet config message:", err)
				continue
			}
			ln.HandleFaucetConfig(&configMsg)
		}
	}
}

func (ln *Libp2pNetwork) HandleFaucetWhitelistTopic(ctx context.Context, sub *pubsub.Subscription) {
	logx.Info("NETWORK:FAUCET", "Starting faucet whitelist topic handler")

	for {
		select {
		case <-ctx.Done():
			logx.Info("NETWORK:FAUCET", "Stopping faucet whitelist topic handler")
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				logx.Error("NETWORK:FAUCET", "Error reading faucet whitelist message", err)
				continue
			}

			if msg.ReceivedFrom == ln.host.ID() {
				continue
			}

			logx.Debug("NETWORK:FAUCET", "Received faucet whitelist message", "size", len(msg.Data))
			whiteMsg := faucet.FaucetSyncWhitelistMessage{}
			if err := jsonx.Unmarshal(msg.Data, &whiteMsg); err != nil {
				logx.Error("NETWORK:FAUCET", "Failed to unmarshal faucet whitelist message:", err)
				continue
			}

			ln.HandleFaucetWhitelist(&whiteMsg)
		}
	}
}

func (ln *Libp2pNetwork) HandleFaucetVoteTopic(ctx context.Context, sub *pubsub.Subscription) {
	logx.Info("NETWORK:FAUCET", "Starting faucet vote topic handler")

	for {
		select {
		case <-ctx.Done():
			logx.Info("NETWORK:FAUCET", "Stopping faucet vote topic handler")
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				logx.Error("NETWORK:FAUCET", "Error reading faucet vote message", err)
				continue
			}

			if msg.ReceivedFrom == ln.host.ID() {
				continue
			}

			var voteMsg FaucetVoteMessage
			if err := jsonx.Unmarshal(msg.Data, &voteMsg); err != nil {
				logx.Error("NETWORK:FAUCET", "Failed to unmarshal faucet vote message:", err)
				continue
			}

			if ln.OnFaucetVote != nil {
				ln.OnFaucetVote(voteMsg.TxHash, voteMsg.VoterID, voteMsg.Approve)
			}
		}
	}
}

func (ln *Libp2pNetwork) HandleRequestFaucetVoteTopic(ctx context.Context, sub *pubsub.Subscription) error {
	logx.Info("NETWORK:FAUCET", "Starting request faucet vote topic handler")

	for {
		select {
		case <-ctx.Done():
			logx.Info("NETWORK:FAUCET", "Stopping request faucet vote topic handler")
			return nil
		default:
			msg, err := sub.Next(ctx)
			logx.Info("NETWORK:FAUCET", "Waiting for request faucet vote message...", msg)
			if err != nil {
				logx.Error("NETWORK:FAUCET", "Error reading request faucet vote message", err)
				continue
			}

			if msg.ReceivedFrom == ln.host.ID() {
				continue
			}

			var requestMsg faucet.RequestFaucetVoteMessage
			if err := jsonx.Unmarshal(msg.Data, &requestMsg); err != nil {
				logx.Error("NETWORK:FAUCET", "Failed to unmarshal request faucet vote message:", err)
				continue
			}

			if !ln.verifySignature(&requestMsg) {
				logx.Warn("NETWORK:FAUCET", "Received request faucet vote with invalid signature", "txHash", requestMsg.TxHash)
				continue
			}

			verified := ln.VerifyVote(&requestMsg)

			if !verified {
				logx.Warn("NETWORK:FAUCET", "Received invalid faucet vote request", "txHash", requestMsg.TxHash)
				continue
			}

			data, err := jsonx.Marshal(&FaucetVoteMessage{
				TxHash:  requestMsg.TxHash,
				VoterID: ln.host.ID(),
				Approve: requestMsg.Approve,
			})

			if err != nil {
				logx.Error("NETWORK:FAUCET", "Failed to marshal faucet vote message", err)
				continue
			}

			if err := ln.topicFaucetVote.Publish(ln.ctx, data); err != nil {
				logx.Error("NETWORK:FAUCET", "Failed to publish faucet vote message", err)
			}
		}
	}
}

func (ln *Libp2pNetwork) HandleRequestInitFaucetConfigTopic(ctx context.Context, sub *pubsub.Subscription) {
	logx.Info("NETWORK:FAUCET", "Starting request init faucet config topic handler")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					logx.Info("NETWORK:FAUCET", "Stopping request init faucet config topic handler")
					return
				}
				continue
			}

			if msg.ReceivedFrom == ln.host.ID() {
				continue
			}

			config := ln.GetFaucetConfig()
			if config == nil {
				logx.Error("NETWORK:FAUCET", "Failed to get faucet config")
				continue
			}

			data, err := jsonx.Marshal(config)
			if err != nil {
				logx.Error("NETWORK:FAUCET", "Failed to marshal faucet config", err)
				continue
			}

			if err := ln.topicInitFaucetConfig.Publish(ln.ctx, data); err != nil {
				logx.Error("NETWORK:FAUCET", "Failed to publish init faucet config message", err)
			}
		}
	}
}

func (ln *Libp2pNetwork) HandleInitFaucetConfigTopic(ctx context.Context, sub *pubsub.Subscription) {
	logx.Info("NETWORK:FAUCET", "Starting init faucet config topic handler")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					logx.Info("NETWORK:FAUCET", "Stopping init faucet config topic handler")
					return
				}
				continue
			}

			if msg.ReceivedFrom == ln.host.ID() {
				continue
			}
			var configMsg faucet.RequestInitFaucetConfigMessage
			if err := jsonx.Unmarshal(msg.Data, &configMsg); err != nil {
				logx.Error("NETWORK:FAUCET", "Failed to unmarshal init faucet config message:", err)
				continue
			}

			if len(configMsg.Approvers) > 0 {
				logx.Info("NETWORK:FAUCET", "Received init faucet config message", "config", configMsg)
				if err := ln.HandleInitFaucetConfig(&configMsg); err != nil {
					logx.Error("NETWORK:FAUCET", "Failed to handle init faucet config message", err)
					continue
				}
			}

		}

	}
}

func (ln *Libp2pNetwork) PublishFaucetMultisigTx(data []byte) error {
	if ln.topicFaucetMultisigTx == nil {
		return fmt.Errorf("faucet multisig tx topic not initialized")
	}
	return ln.topicFaucetMultisigTx.Publish(ln.ctx, data)
}

func (ln *Libp2pNetwork) PublishFaucetConfig(data []byte) error {
	if ln.topicFaucetConfig == nil {
		return fmt.Errorf("faucet config topic not initialized")
	}
	return ln.topicFaucetConfig.Publish(ln.ctx, data)
}

func (ln *Libp2pNetwork) PublishFaucetWhitelist(data []byte) error {
	if ln.topicFaucetWhitelist == nil {
		return fmt.Errorf("faucet whitelist topic not initialized")
	}
	return ln.topicFaucetWhitelist.Publish(ln.ctx, data)
}

func (ln *Libp2pNetwork) PublicRequestFaucetVote(data []byte) error {
	if ln.topicRequestFaucetVote == nil {
		return fmt.Errorf("request faucet vote topic not initialized")
	}
	return ln.topicRequestFaucetVote.Publish(ln.ctx, data)
}

func (ln *Libp2pNetwork) verifySignature(msg *faucet.RequestFaucetVoteMessage) bool {
	pubKey, err := getPublicKeyFromLeaderID(msg.LeaderId)
	if err != nil {
		logx.Error("BroadcastedBlock", fmt.Sprintf("Failed to get public key from LeaderID: %v", err))
		return false
	}

	if len(msg.Signature) != ed25519.SignatureSize {
		logx.Warn("BLOCK", "verify block signature failure different length of signature")
		return false
	}

	return ed25519.Verify(pubKey, []byte(msg.TxHash), msg.Signature)
}

func getPublicKeyFromLeaderID(leaderID string) (ed25519.PublicKey, error) {
	if leaderID == "" {
		return nil, errors.New("leader ID cannot be empty")
	}

	pubKeyBytes, err := common.DecodeBase58ToBytes(leaderID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode leader ID")
	}

	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return nil, errors.Errorf("invalid leader ID length: expected %d, got %d", ed25519.PublicKeySize, len(pubKeyBytes))
	}

	return ed25519.PublicKey(pubKeyBytes), nil
}
