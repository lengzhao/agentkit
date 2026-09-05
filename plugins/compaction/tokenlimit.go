package compaction

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	rtcompaction "github.com/lengzhao/agentkit/runtime/compaction"
	"github.com/lengzhao/agentkit/runtime/session"
)

type TokenLimitConfig struct {
	// MaxTokens is absolute trigger; takes precedence over ContextWindow.
	MaxTokens int `json:"maxTokens"`
	// ContextWindow is model context size; the trigger becomes ContextWindow × TriggerRatio.
	ContextWindow int `json:"contextWindow"`
	// TriggerRatio is fraction of ContextWindow that trips compaction, default 0.7 — leaving room for the reply plus the next tool result.
	TriggerRatio float64 `json:"triggerRatio"`
	// CharsPerToken calibrates the fallback estimate used before the provider reports usage; default 4 (English prose), lower it for CJK.
	CharsPerToken int `json:"charsPerToken"`
}

type TokenLimitDeps struct {
	// Services run only once the threshold is crossed, with Force set.
	Services []compaction.Service `json:"services"`
}

const (
	defaultTriggerRatio  = 0.7
	defaultCharsPerToken = 4
)

type tokenLimitService struct {
	threshold     int
	charsPerToken int
	services      []compaction.Service
}

// NewTokenLimit registers compaction/token-limit: Trigger inner compaction services once the context crosses a token threshold.
//
// Best practices:
//   - A decorator, not a strategy: it decides when, the services dep decides how.
//   - The estimate is max(character estimate, reported usage), so it errs toward compacting early.
func NewTokenLimit(cfg TokenLimitConfig, deps TokenLimitDeps) (compaction.Service, error) {
	ratio := cfg.TriggerRatio
	if ratio <= 0 || ratio >= 1 {
		ratio = defaultTriggerRatio
	}
	threshold := cfg.MaxTokens
	if threshold <= 0 && cfg.ContextWindow > 0 {
		threshold = int(float64(cfg.ContextWindow) * ratio)
	}
	if threshold <= 0 {
		return nil, fmt.Errorf("compaction/token-limit requires maxTokens or contextWindow")
	}
	charsPerToken := cfg.CharsPerToken
	if charsPerToken <= 0 {
		charsPerToken = defaultCharsPerToken
	}
	var services []compaction.Service
	for _, svc := range deps.Services {
		if svc != nil {
			services = append(services, svc)
		}
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("compaction/token-limit requires at least one service to gate")
	}
	return &tokenLimitService{
		threshold:     threshold,
		charsPerToken: charsPerToken,
		services:      services,
	}, nil
}

func (s *tokenLimitService) Compact(ctx context.Context, req compaction.Request) (compaction.Result, error) {
	estimate := s.estimate(ctx, req)
	if !req.Force && estimate < s.threshold {
		return compaction.Result{}, nil
	}
	if !req.Force {
		slog.Info("token threshold reached, compacting",
			"session_id", req.SessionID,
			"agent_id", req.AgentID,
			"estimated_tokens", estimate,
			"threshold", s.threshold,
		)
	}

	inner := req
	inner.Force = true
	messages, applied, err := rtcompaction.ApplyAll(ctx, s.services, inner)
	if err != nil {
		return compaction.Result{}, err
	}
	return compaction.Result{Applied: applied > 0, Messages: messages}, nil
}

// estimate takes the larger of two imperfect signals. The provider's last
// reported prompt size is exact but one step stale; the character heuristic
// covers the current history but misjudges dense or non-Latin content. Taking
// the max errs toward compacting slightly early, which is the cheap mistake.
func (s *tokenLimitService) estimate(ctx context.Context, req compaction.Request) int {
	heuristic := charEstimate(req.Messages, s.charsPerToken)
	if req.Session != nil {
		if events, err := session.ReadAllEvents(ctx, req.Session); err == nil {
			logical := session.SumLogicalCharsFromEvents(events, req.AgentID) / s.charsPerToken
			if logical > heuristic {
				heuristic = logical
			}
		}
	}
	reported := s.reportedTokens(ctx, req)
	if reported > heuristic {
		return reported
	}
	return heuristic
}

// reportedTokens reads the most recent usage event: prompt plus reply is what
// the next request would carry forward if nothing were compacted.
func (s *tokenLimitService) reportedTokens(ctx context.Context, req compaction.Request) int {
	if req.Session == nil {
		return 0
	}
	events, err := session.ReadAllEvents(ctx, req.Session)
	if err != nil {
		return 0
	}
	usage := session.LatestUsage(events)
	return usage.InputTokens + usage.OutputTokens
}

func charEstimate(messages []agentkit.ModelMessage, charsPerToken int) int {
	chars := 0
	for _, msg := range messages {
		chars += len(msg.Role)
		chars += partsChars(msg.Content)
		for _, call := range msg.ToolCalls {
			chars += len(call.Name) + len(call.Input)
		}
		for _, result := range msg.ToolResults {
			chars += len(result.Name) + len(result.Content)
		}
	}
	return chars / charsPerToken
}

func partsChars(parts []agentkit.ContentPart) int {
	chars := 0
	for _, part := range parts {
		chars += len(part.Text) + len(part.Source) + len(part.URL)
	}
	return chars
}
