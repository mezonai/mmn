package jsonrpc

import (
	"net"
	"net/http"
	"strings"

	"github.com/mezonai/mmn/logx"
)

func extractClientIPFromRequest(r *http.Request) string {
	logx.Debug("Extracting client IP from request headers", "X-Real-IP", r.Header.Get("X-Real-IP"), "X-Forwarded-For", r.Header.Get("X-Forwarded-For"), "RemoteAddr", r.RemoteAddr, "CF-Connecting-IP", r.Header.Get("CF-Connecting-IP"))
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		ip := strings.TrimSpace(xri)
		if net.ParseIP(ip) != nil {
			return ip
		}
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && net.ParseIP(host) != nil {
		return host
	}
	return "unknown"
}
