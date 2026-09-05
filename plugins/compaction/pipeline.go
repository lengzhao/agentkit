package compaction

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit/cap/compaction"
	rtcompaction "github.com/lengzhao/agentkit/runtime/compaction"
)

type PipelineDeps struct {
	Services []compaction.Service `json:"services"`
}

type pipelineService struct {
	services []compaction.Service
}

// NewPipeline registers compaction/pipeline: Run inner compaction services in order as one unit.
func NewPipeline(_ struct{}, deps PipelineDeps) (compaction.Service, error) {
	var services []compaction.Service
	for _, svc := range deps.Services {
		if svc != nil {
			services = append(services, svc)
		}
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("compaction/pipeline requires at least one service")
	}
	return &pipelineService{services: services}, nil
}

func (p *pipelineService) Compact(ctx context.Context, req compaction.Request) (compaction.Result, error) {
	messages, applied, err := rtcompaction.ApplyAll(ctx, p.services, req)
	if err != nil {
		return compaction.Result{}, err
	}
	return compaction.Result{Applied: applied > 0, Messages: messages}, nil
}
