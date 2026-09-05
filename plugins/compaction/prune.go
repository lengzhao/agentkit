package compaction

import (
	"context"

	"github.com/lengzhao/agentkit/cap/compaction"
	rtcompaction "github.com/lengzhao/agentkit/runtime/compaction"
)

type PruneConfig struct {
	// MaxToolResultBytes is per-result truncation limit.
	MaxToolResultBytes int `json:"maxToolResultBytes"`
}

type pruneService struct {
	maxBytes int
}

// NewPrune registers compaction/prune-tool-results: Trim verbose tool results without calling a model.
//
// Best practices:
//   - Cheap and lossless enough to run before compaction/summary in the same chain.
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
		Messages: rtcompaction.PruneToolResults(req.Messages, s.maxBytes),
	}, nil
}
