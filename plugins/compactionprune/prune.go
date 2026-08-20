package compactionprune

import (
	"context"

	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	MaxToolResultBytes int `json:"maxToolResultBytes"`
}

type Service struct {
	maxBytes int
}

func init() {
	pluginkit.Register("compaction/prune-tool-results", New)
}

func New(cfg Config) (compaction.Service, error) {
	maxBytes := cfg.MaxToolResultBytes
	if maxBytes <= 0 {
		maxBytes = 8192
	}
	return &Service{maxBytes: maxBytes}, nil
}

func (s *Service) Compact(_ context.Context, req compaction.Request) (compaction.Result, error) {
	return compaction.Result{
		Applied:  true,
		Messages: compaction.PruneToolResults(req.Messages, s.maxBytes),
	}, nil
}
