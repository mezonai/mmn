package p2p

import (
	"context"
	"encoding/json"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/logx"
)

func (ln *Libp2pNetwork) HandleCertTopic(ctx context.Context, sub *pubsub.Subscription) {
	for {
		select {
		case <-ctx.Done():
			logx.Info("NETWORK:CERT", "Stopping cert topic handler")
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logx.Warn("NETWORK:CERT", "Next error:", err)
				continue
			}

			var certMsg CertMessage
			if err := json.Unmarshal(msg.Data, &certMsg); err != nil {
				logx.Warn("NETWORK:CERT", "Unmarshal error:", err)
				continue
			}

			cert := ln.ConvertMessageToCert(certMsg)
			if cert != nil && ln.onCertReceived != nil {
				ln.onCertReceived(cert)
			}
		}
	}
}

func (ln *Libp2pNetwork) BroadcastCert(ctx context.Context, cert *consensus.Cert) error {
	msg := CertMessage{
		Slot:                 cert.Slot,
		CertType:             int(cert.CertType),
		BlockHash:            cert.BlockHash,
		Stake:                cert.Stake,
		AggregateSig:         cert.AggregateSig,
		AggregateSigFallback: cert.AggregateSigFallback,
		ListPubKeys:          cert.ListPubKeys,
		ListPubKeysFallback:  cert.ListPubKeysFallback,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	if ln.topicCerts != nil {
		ln.topicCerts.Publish(ctx, data)
	}
	return nil
}
