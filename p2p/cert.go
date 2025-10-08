package p2p

import (
	"context"
	"encoding/json"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/jsonx"
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

			var cert *consensus.Cert
			if err := json.Unmarshal(msg.Data, &cert); err != nil {
				logx.Warn("NETWORK:CERT", "Unmarshal error:", err)
				continue
			}

			if ln.onCertReceived != nil {
				ln.onCertReceived(cert)
			}
		}
	}
}

func (ln *Libp2pNetwork) BroadcastCert(ctx context.Context, cert *consensus.Cert) error {
	data, err := jsonx.Marshal(cert)
	if err != nil {
		return err
	}

	if ln.topicCerts != nil {
		ln.topicCerts.Publish(ctx, data)
	}
	return nil
}
