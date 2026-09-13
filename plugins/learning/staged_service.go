package learning

import (
	"context"

	rtlearning "github.com/lengzhao/agentkit/runtime/learning"
)

func (s *Service) reviewNudgeStore(ctx context.Context) (*rtlearning.NudgeStore, error) {
	path, err := s.memory.ResolveRel(ctx, DefaultDreamingSubdir, "review_nudge.json")
	if err != nil {
		return nil, err
	}
	return &rtlearning.NudgeStore{Path: path}, nil
}

func (s *Service) reviewQuotaStore(ctx context.Context) (*rtlearning.QuotaStore, error) {
	path, err := s.memory.ResolveRel(ctx, DefaultDreamingSubdir, "review_quota.json")
	if err != nil {
		return nil, err
	}
	return &rtlearning.QuotaStore{Path: path}, nil
}
