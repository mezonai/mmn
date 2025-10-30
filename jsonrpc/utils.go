package jsonrpc

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/mezonai/mmn/logx"
)

// JSON-RPC Method name constants
const (
	// Transaction methods
	MethodTxAddTx                  = "tx.addtx"
	MethodTxGetTxByHash            = "tx.gettxbyhash"
	MethodTxGetTransactionStatus   = "tx.gettransactionstatus"
	MethodTxGetPendingTransactions = "tx.getpendingtransactions"

	// Account methods
	MethodAccountGetAccount          = "account.getaccount"
	MethodAccountGetCurrentNonce     = "account.getcurrentnonce"
	MethodAccountGetAccountByAddress = "account.getaccountbyaddress"

	// Health methods
	MethodHealthCheck = "health.check"
)

func parseJSONRPCRequest(body []byte) *jsonRPCRequest {
	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil
	}
	return &req
}

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
