package compaction

import (
	"context"

	capscompaction "github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit"
)

type chainService struct{}

// NewChain registers compaction/chain: ordered ApplyAll over deps.services (telemetry in runtime ApplyAll).
func NewChain(_ struct{}) (capscompaction.Chain, error) {
	return chainService{}, nil
}

func (chainService) ApplyAll(ctx context.Context, services []capscompaction.Service, req capscompaction.Request) ([]agentkit.ModelMessage, int, error) {
	return ApplyAll(ctx, services, req)
}
