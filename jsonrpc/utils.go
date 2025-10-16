package jsonrpc

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

func parseJSONRPCMethodAndWallet(body []byte) (string, string) {
	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return "", "unknown"
	}

	if !isTxJSONRPCMethod(req.Method) {
		return req.Method, "unknown"
	}

	var p signedTxParams
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, &p)
		if p.TxMsg.Sender != "" {
			return req.Method, p.TxMsg.Sender
		}
	}
	return req.Method, "unknown"
}

func isTxJSONRPCMethod(method string) bool {
	switch strings.ToLower(method) {
	case "tx.addtx":
		return true
	default:
		return false
	}
}

func extractClientIPFromRequest(r *http.Request) string {
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
