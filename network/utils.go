package network

import (
	"context"
	"net"

	"google.golang.org/grpc/peer"
)

func extractClientIP(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "unknown"
	}

	addr := p.Addr.String()
	if addr == "" {
		return "unknown"
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		if net.ParseIP(addr) != nil {
			return addr
		}
		return "unknown"
	}

	if len(host) > 0 && host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	}

	return host
}
