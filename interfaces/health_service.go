package interfaces

import (
	"context"

	pb "github.com/mezonai/mmn/proto"
)

type HealthService interface {
	Check(ctx context.Context) (*pb.HealthCheckResponse, error)
}
