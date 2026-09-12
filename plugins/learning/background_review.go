package learning

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	capsessionindex "github.com/lengzhao/agentkit/cap/sessionindex"
	rtlearning "github.com/lengzhao/agentkit/runtime/learning"
	"github.com/lengzhao/agentkit/runtime/session"
	rttools "github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/pluginkit"
)

// BackgroundReviewConfig controls post-turn LLM review (Hermes-style learning fork).
type BackgroundReviewConfig struct {
	Enabled           *bool  `json:"enabled"`
	Model             string `json:"model"`
	MaxSteps          int    `json:"maxSteps"`
	MaxDigestMessages int    `json:"maxDigestMessages"`
	SkipSlashOnly     *bool  `json:"skipSlashOnly"`
	MinTurnTokens     int    `json:"minTurnTokens"`
	MaxReviewsPerDay  int    `json:"maxReviewsPerDay"`
	MinIdleSeconds    int    `json:"minIdleSeconds"`
	Notify            bool   `json:"notify"`
}

type backgroundReviewDeps struct {
	Learning     *Service                `json:"learning"`
	LLM          agentkit.LLMProvider    `json:"llm"`
	CaptureTool  agentkit.Tool           `json:"captureTool"`
	SessionIndex capsessionindex.Service `json:"sessionIndex,omitempty"`
}

type backgroundReviewProvider struct {
	cfg      BackgroundReviewConfig
	learning *Service
	llm      agentkit.LLMProvider
	tools    agentkit.ToolRuntime
	index    capsessionindex.Service
	idle     sync.Map // sessionID -> last turn complete time
}

func init() {
	pluginkit.Register("hook/background-review", NewBackgroundReview)
	pluginkit.Register("tool/learn-capture", NewLearnCapture)
}

// NewBackgroundReview registers hook/background-review: spawn an LLM review fork after each successful turn.
func NewBackgroundReview(cfg BackgroundReviewConfig, deps backgroundReviewDeps) (agentkit.HookProvider, error) {
	if deps.Learning == nil {
		return nil, fmt.Errorf("hook/background-review requires learning")
	}
	if deps.LLM == nil {
		return nil, fmt.Errorf("hook/background-review requires llm")
	}
	if deps.CaptureTool == nil {
		return nil, fmt.Errorf("hook/background-review requires captureTool")
	}
	rt, err := rttools.NewRuntime(rttools.RuntimeConfig{
		AllowTools:            []string{"learn_capture"},
		DefaultTimeoutSeconds: 120,
	}, rttools.RuntimeDeps{
		Tools: []agentkit.Tool{deps.CaptureTool},
	})
	if err != nil {
		return nil, err
	}
	p := &backgroundReviewProvider{
		cfg:      cfg,
		learning: deps.Learning,
		llm:      deps.LLM,
		tools:    rt,
		index:    deps.SessionIndex,
	}
	return p, nil
}

func (p *backgroundReviewProvider) Hooks() []agentkit.Hook {
	return []agentkit.Hook{agentkit.OnTurnComplete(p.onTurnComplete)}
}

func (p *backgroundReviewProvider) onTurnComplete(ctx context.Context, tc *agentkit.TurnComplete) error {
	if tc == nil || p.learning == nil || p.learning.disabled {
		return nil
	}
	if !backgroundReviewEnabled(p.cfg) {
		return nil
	}
	skipSlash := true
	if p.cfg.SkipSlashOnly != nil {
		skipSlash = *p.cfg.SkipSlashOnly
	}
	if !shouldRunReview(tc.Messages, skipSlash) {
		return nil
	}
	if p.cfg.MinTurnTokens > 0 && tc.TurnTokens < p.cfg.MinTurnTokens {
		return nil
	}
	if p.cfg.MinIdleSeconds > 0 {
		key := string(tc.SessionID)
		if last, ok := p.idle.Load(key); ok {
			if t, ok := last.(time.Time); ok && time.Since(t) < time.Duration(p.cfg.MinIdleSeconds)*time.Second {
				return nil
			}
		}
	}
	model := strings.TrimSpace(p.cfg.Model)
	if model == "" {
		model = strings.TrimSpace(tc.Model)
	}
	if model == "" {
		return nil
	}

	parent := context.WithoutCancel(ctx)
	p.idle.Store(string(tc.SessionID), time.Now())

	runs := rtlearning.GlobalReviewRuns()
	reviewCtx, cancel := runs.Begin(parent, string(tc.SessionID))
	go func() {
		defer cancel()
		defer runs.End(string(tc.SessionID))
		runCtx := session.WithWorkspaceService(reviewCtx, p.learning.workspace)
		runCtx = session.WithConversation(runCtx, string(tc.SessionID))
		runCtx = session.WithAgentID(runCtx, tc.AgentID)

		if p.cfg.MaxReviewsPerDay > 0 {
			ok, err := p.learning.tryConsumeReviewQuota(runCtx, p.cfg.MaxReviewsPerDay)
			if err != nil {
				slog.Warn("background review quota check failed", "err", err)
				return
			}
			if !ok {
				slog.Debug("background review skipped: daily quota",
					"session_id", tc.SessionID, "max", p.cfg.MaxReviewsPerDay)
				return
			}
		}

		recall := p.sessionRecall(runCtx, tc.Messages)
		start := time.Now()
		result, err := rtlearning.RunReview(runCtx, rtlearning.ReviewConfig{
			MaxSteps:          p.cfg.MaxSteps,
			MaxDigestMessages: p.cfg.MaxDigestMessages,
			SessionRecall:     recall,
		}, p.llm, p.tools, model, tc.Messages)
		if err != nil {
			if runCtx.Err() != nil {
				slog.Debug("background review cancelled",
					"session_id", tc.SessionID, "agent_id", tc.AgentID)
				return
			}
			slog.Warn("background review failed",
				"session_id", tc.SessionID, "agent_id", tc.AgentID, "err", err)
			return
		}
		summary := ""
		steps := 0
		if result != nil {
			summary = result.Summary
			steps = result.Steps
		}
		attrs := []any{
			"session_id", tc.SessionID,
			"agent_id", tc.AgentID,
			"duration", time.Since(start),
			"steps", steps,
		}
		if result != nil {
			attrs = append(attrs, "input_tokens", result.InputTokens, "output_tokens", result.OutputTokens)
		}
		if summary != "" {
			attrs = append(attrs, "summary", truncateLog(summary, 200))
		}
		slog.Info("background review complete", attrs...)
		if p.cfg.Notify && summary != "" {
			slog.Info("learning notification", "message", "💾 "+truncateLog(summary, 160))
		}
	}()
	return nil
}

func (s *Service) tryConsumeReviewQuota(ctx context.Context, maxPerDay int) (bool, error) {
	store, err := s.reviewQuotaStore(ctx)
	if err != nil {
		return false, err
	}
	return store.TryConsume(maxPerDay, time.Now().UTC())
}

func backgroundReviewEnabled(cfg BackgroundReviewConfig) bool {
	if cfg.Enabled == nil {
		return true
	}
	return *cfg.Enabled
}

func truncateLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func (p *backgroundReviewProvider) sessionRecall(ctx context.Context, messages []agentkit.ModelMessage) string {
	if p.index == nil || p.learning == nil {
		return ""
	}
	query := reviewRecallQuery(messages)
	if query == "" {
		return ""
	}
	dir, err := p.learning.workspace.Resolve(ctx, p.learning.sessionsDir)
	if err != nil {
		return ""
	}
	if err := p.index.SyncSessions(ctx, dir); err != nil {
		return ""
	}
	hits, err := p.index.Search(ctx, query, 5)
	if err != nil {
		return ""
	}
	return rtlearning.FormatSessionRecall(hits)
}

func reviewRecallQuery(messages []agentkit.ModelMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		text := strings.TrimSpace(flattenParts(messages[i].Content))
		if text == "" || strings.HasPrefix(text, "/") {
			continue
		}
		return truncateLog(text, 160)
	}
	return ""
}
