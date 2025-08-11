package p2p

import (
	"crypto/ed25519"
	"fmt"

	"github.com/libp2p/go-libp2p/core/crypto"
	ma "github.com/multiformats/go-multiaddr"
)

func UnmarshalEd25519PrivateKey(private ed25519.PrivateKey) (crypto.PrivKey, error) {
	if len(private) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid ed25519 private key length")
	}
	seed := private[:32]
	return crypto.UnmarshalEd25519PrivateKey(seed)
}

func (ln *Libp2pNetwork) GetOwnAddress() string {
	addrs := ln.host.Addrs()
	if len(addrs) > 0 {
		return fmt.Sprintf("%s/p2p/%s", addrs[0].String(), ln.host.ID().String())
	}
	return ""
}

func AddrStrings(addrs []ma.Multiaddr) []string {
	var strAddrs []string
	for _, addr := range addrs {
		strAddrs = append(strAddrs, addr.String())
	}
	return strAddrs
}
