package discovery

import (
	"context"
	"time"

	libp2p_peer "github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
)

func ResolveAndParseMultiAddrs(addrStrings []string) ([]libp2p_peer.AddrInfo, error) {
	var res []libp2p_peer.AddrInfo
	for _, addrStr := range addrStrings {
		ais, err := resolveMultiAddrString(addrStr)
		if err != nil {
			return nil, err
		}
		res = append(res, ais...)
	}
	return res, nil
}

func resolveMultiAddrString(addrStr string) ([]libp2p_peer.AddrInfo, error) {
	var ais []libp2p_peer.AddrInfo

	mAddr, err := ma.NewMultiaddr(addrStr)
	if err != nil {
		return nil, err
	}
	mAddrs, err := resolveMultiAddr(mAddr)
	if err != nil {
		return nil, err
	}
	for _, mAddr := range mAddrs {
		ai, err := libp2p_peer.AddrInfoFromP2pAddr(mAddr)
		if err != nil {
			return nil, err
		}
		ais = append(ais, *ai)
	}
	return ais, nil
}

func resolveMultiAddr(raw ma.Multiaddr) ([]ma.Multiaddr, error) {
	if madns.Matches(raw) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		mas, err := madns.Resolve(ctx, raw)
		if err != nil {
			return nil, err
		}
		return mas, nil
	}
	return []ma.Multiaddr{raw}, nil
}
