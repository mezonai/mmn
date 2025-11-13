package network

import "time"

const (
	// This prevents hung requests.
	GRPCDefaultDeadline = 60 * time.Second
)
