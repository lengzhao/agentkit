package compaction

import (
	"context"

	"github.com/lengzhao/agentkit/cap/compaction"
)

type PruneConfig struct {
	MaxToolResultBytes int `json:"maxToolResultBytes"`
}

type pruneService struct {
	maxBytes int
}

func NewPrune(cfg PruneConfig) (compaction.Service, error) {
	maxBytes := cfg.MaxToolResultBytes
	if maxBytes <= 0 {
		maxBytes = 8192
	}
	return &pruneService{maxBytes: maxBytes}, nil
}

func (s *pruneService) Compact(_ context.Context, req compaction.Request) (compaction.Result, error) {
	return compaction.Result{
		Applied:  true,
		Messages: compaction.PruneToolResults(req.Messages, s.maxBytes),
	}, nil
}
