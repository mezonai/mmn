package network

import "time"

const (
	// This prevents hung requests.
	GRPCDefaultDeadline = 30 * time.Second

	// The maximum inbound gRPC message size (bytes)
	GRPCMaxRecvMsgSize = 4 * 1024 * 1024
	// The maximum outbound gRPC message size (bytes)
	GRPCMaxSendMsgSize = 20 * 1024 * 1024
)
