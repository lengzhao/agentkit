package compaction

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit/cap/compaction"
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
// SetDefaults implements pluginkit.Defaulter.
func (c *PruneConfig) SetDefaults() {
	if c.MaxToolResultBytes == 0 {
		c.MaxToolResultBytes = 8192
	}
}

// Validate implements pluginkit.Validator.
func (c *PruneConfig) Validate() error {
	if c.MaxToolResultBytes < 0 {
		return fmt.Errorf("compaction/prune-tool-results maxToolResultBytes must not be negative")
	}
	return nil
}

func NewPrune(cfg PruneConfig) (compaction.Service, error) {
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &pruneService{maxBytes: cfg.MaxToolResultBytes}, nil
}

func (s *pruneService) Compact(_ context.Context, req compaction.Request) (compaction.Result, error) {
	messages, pruned := PruneToolResultsReport(req.Messages, s.maxBytes)
	return compaction.Result{
		Applied:  pruned,
		Messages: messages,
	}, nil
}
