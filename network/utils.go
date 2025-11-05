package network

import (
	"context"
	"net"

	"google.golang.org/grpc/peer"
)

const unknownIP = "unknown"

func extractClientIP(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return unknownIP
	}

	addr := p.Addr.String()
	if addr == "" {
		return unknownIP
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		if net.ParseIP(addr) != nil {
			return addr
		}
		return unknownIP
	}

	if host != "" && host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	}

	return host
}
