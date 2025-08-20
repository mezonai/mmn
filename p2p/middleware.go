package p2p

import (
	"github.com/libp2p/go-libp2p/core/network"
)

func (ln *Libp2pNetwork) AuthMiddleware(handler func(network.Stream)) func(network.Stream) {
	return func(s network.Stream) {
		remotePeer := s.Conn().RemotePeer()

		if !ln.IsPeerAuthenticated(remotePeer) && ln.allowlistEnabled {
			ln.challengeMu.RLock()
			_, hasPending := ln.pendingChallenges[remotePeer]
			ln.challengeMu.RUnlock()

			if hasPending {
				handler(s)
				return
			}
			s.Close()
			return
		}

		handler(s)
	}
}
