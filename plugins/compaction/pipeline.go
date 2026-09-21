package compaction

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit/cap/compaction"
)

type PipelineDeps struct {
	Chain    compaction.Chain     `json:"chain"`
	Services []compaction.Service `json:"services"`
}

type pipelineService struct {
	chain    compaction.Chain
	services []compaction.Service
}

// NewPipeline registers compaction/pipeline: Run inner compaction services in order as one unit.
func NewPipeline(_ struct{}, deps PipelineDeps) (compaction.Service, error) {
	if deps.Chain == nil {
		return nil, fmt.Errorf("compaction/pipeline requires chain dependency")
	}
	var services []compaction.Service
	for _, svc := range deps.Services {
		if svc != nil {
			services = append(services, svc)
		}
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("compaction/pipeline requires at least one service")
	}
	return &pipelineService{chain: deps.Chain, services: services}, nil
}

func (p *pipelineService) Compact(ctx context.Context, req compaction.Request) (compaction.Result, error) {
	messages, applied, err := p.chain.ApplyAll(ctx, p.services, req)
	if err != nil {
		return compaction.Result{}, err
	}
	return compaction.Result{Applied: applied > 0, Messages: messages}, nil
}
