package main

import "github.com/multiformats/go-multiaddr"

func addrStrings(addrs []multiaddr.Multiaddr) []string {
	var strAddrs []string
	for _, addr := range addrs {
		strAddrs = append(strAddrs, addr.String())
	}
	return strAddrs
}
