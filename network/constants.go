package network

import "time"

const (
	// GRPCDefaultDeadline defines the default per-RPC timeout applied on the server
	// when the client does not supply any deadline. This prevents hung requests.
	GRPCDefaultDeadline = 30 * time.Second

	// GRPCMaxRecvMsgSize caps the maximum inbound gRPC message size (bytes)
	GRPCMaxRecvMsgSize = 4 * 1024 * 1024
	// GRPCMaxSendMsgSize caps the maximum outbound gRPC message size (bytes)
	GRPCMaxSendMsgSize = 20 * 1024 * 1024

	// GRPCRateLimitRPS is the allowed average requests-per-second across the server
	GRPCRateLimitRPS = 5000
	// GRPCRateLimitBurst is the short-term burst capacity on top of RPS
	GRPCRateLimitBurst = 10000
)
