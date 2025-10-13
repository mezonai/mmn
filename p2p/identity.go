package p2p

import (
	"encoding/base64"
	"encoding/hex"

	"github.com/libp2p/go-libp2p/core/peer"
)

// peerMatchesID reports whether the authenticated public key string for peerID matches idStr
// Comparison tries base64 and hex encodings of the ed25519 public key, and raw string equality.
func (ln *Libp2pNetwork) peerMatchesID(peerID peer.ID, idStr string) bool {
	if idStr == "" {
		return false
	}
	ln.authMu.RLock()
	auth, ok := ln.authenticatedPeers[peerID]
	ln.authMu.RUnlock()
	if !ok || auth == nil || len(auth.PublicKey) == 0 {
		return false
	}

	b64 := base64.StdEncoding.EncodeToString(auth.PublicKey)
	if b64 == idStr {
		return true
	}
	h := hex.EncodeToString(auth.PublicKey)
	if h == idStr {
		return true
	}
	// As a last resort, direct byte-to-string comparison
	if string(auth.PublicKey) == idStr {
		return true
	}
	return false
}
