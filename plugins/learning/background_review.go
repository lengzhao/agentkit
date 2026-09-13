package learning

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
	caplearning "github.com/lengzhao/agentkit/cap/learning"
	capmemory "github.com/lengzhao/agentkit/cap/memory"
	capsessionindex "github.com/lengzhao/agentkit/cap/sessionindex"
	rtdelivery "github.com/lengzhao/agentkit/runtime/delivery"
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
	MinIdleSeconds      int    `json:"minIdleSeconds"`
	MemoryNotifications string `json:"memoryNotifications"` // off | on | verbose (Hermes display.memory_notifications)
	// MemoryNudgeInterval user turns between automatic memory reviews (Hermes memory.nudge_interval). 0 disables; default 10.
	MemoryNudgeInterval *int `json:"memoryNudgeInterval"`
	// SessionRecall when false, skip FTS prior-session block in the review digest (use tool/session-search instead).
	SessionRecall *bool `json:"sessionRecall"`
}

// reviewLearning is learning/default: review orchestration + skill_propose (memory is a separate dep).
type reviewLearning interface {
	caplearning.ReviewHost
	caplearning.SkillProposer
}

type backgroundReviewDeps struct {
	Learning     reviewLearning            `json:"learning"`
	Memory       capmemory.Capture         `json:"memory"`
	LLM          agentkit.LLMProvider      `json:"llm"`
	SessionIndex capsessionindex.Service   `json:"sessionIndex,omitempty"`
	Sender       capsdelivery.Sender       `json:"sender,omitempty"`
}

type backgroundReviewProvider struct {
	cfg      BackgroundReviewConfig
	learning reviewLearning
	llm      agentkit.LLMProvider
	tools    agentkit.ToolRuntime
	index    capsessionindex.Service
	sender   capsdelivery.Sender
	idle     sync.Map // sessionID -> last turn complete time
}

func init() {
	pluginkit.Register("hook/background-review", NewBackgroundReview)
}

// NewBackgroundReview registers hook/background-review: spawn an LLM review fork after each successful turn.
func NewBackgroundReview(cfg BackgroundReviewConfig, deps backgroundReviewDeps) (agentkit.HookProvider, error) {
	if deps.Learning == nil {
		return nil, fmt.Errorf("hook/background-review requires learning")
	}
	if deps.Memory == nil {
		return nil, fmt.Errorf("hook/background-review requires memory")
	}
	if deps.LLM == nil {
		return nil, fmt.Errorf("hook/background-review requires llm")
	}
	captureTool, err := NewLearnCaptureTool(deps.Memory, deps.Learning)
	if err != nil {
		return nil, err
	}
	rt, err := rttools.NewRuntime(rttools.RuntimeConfig{
		AllowTools:            []string{"learn_capture"},
		DefaultTimeoutSeconds: 120,
	}, rttools.RuntimeDeps{
		Tools: []agentkit.Tool{captureTool},
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
		sender:   deps.Sender,
	}
	return p, nil
}

func (p *backgroundReviewProvider) Hooks() []agentkit.Hook {
	return []agentkit.Hook{agentkit.OnTurnComplete(p.onTurnComplete)}
}

func (p *backgroundReviewProvider) onTurnComplete(ctx context.Context, tc *agentkit.TurnComplete) error {
	if tc == nil || p.learning == nil || p.learning.Disabled() {
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
	interval := memoryNudgeInterval(p.cfg)
	prior, seen, err := p.learning.ReviewNudgeLoad(ctx, string(tc.SessionID))
	if err != nil {
		slog.Warn("background review nudge load failed", "err", err)
		return nil
	}
	nudgeDec, nextNudge := rtlearning.DecideMemoryNudge(interval, tc.Messages, prior, !seen)
	if err := p.learning.ReviewNudgeSave(ctx, string(tc.SessionID), nextNudge); err != nil {
		slog.Warn("background review nudge save failed", "err", err)
	}
	if !nudgeDec.RunReview {
		slog.Debug("background review skipped",
			"session_id", tc.SessionID,
			"reason", nudgeDec.Reason,
			"turns_since_memory", nudgeDec.TurnsSinceMemory,
			"interval", interval)
		return nil
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
		runCtx := session.WithWorkspaceService(reviewCtx, p.learning.Workspace())
		runCtx = session.ApplyEnvelopeToContext(runCtx, session.EnvelopeFromContext(parent))
		runCtx = session.WithConversation(runCtx, string(tc.SessionID))
		runCtx = session.WithAgentID(runCtx, tc.AgentID)

		if p.cfg.MaxReviewsPerDay > 0 {
			ok, err := p.learning.TryConsumeReviewQuota(runCtx, p.cfg.MaxReviewsPerDay)
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
		candidates := p.learning.ReviewSignalCandidates(runCtx)
		start := time.Now()
		result, err := rtlearning.RunReview(runCtx, rtlearning.ReviewConfig{
			MaxSteps:          p.cfg.MaxSteps,
			MaxDigestMessages: p.cfg.MaxDigestMessages,
			SessionRecall:     recall,
			SignalCandidates:  candidates,
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
			attrs = append(attrs, "summary", rtlearning.TruncateEllipsis(summary, 200))
		}
		slog.Info("background review complete", attrs...)
		notifyMode := NormalizeMemoryNotifications(p.cfg.MemoryNotifications)
		notices := []string{}
		if result != nil {
			notices = result.Notices
		}
		line := FormatBackgroundReviewNotification(notifyMode, notices)
		if line != "" {
			slog.Info("learning notification", "message", line)
			if err := rtdelivery.SendProactiveInboxText(runCtx, p.sender, line); err != nil {
				slog.Debug("learning notification delivery skipped", "err", err)
			}
		}
	}()
	return nil
}

func (s *Service) TryConsumeReviewQuota(ctx context.Context, maxPerDay int) (bool, error) {
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

func memoryNudgeInterval(cfg BackgroundReviewConfig) int {
	if cfg.MemoryNudgeInterval != nil {
		return *cfg.MemoryNudgeInterval
	}
	return rtlearning.DefaultMemoryNudgeInterval
}

func sessionRecallEnabled(cfg BackgroundReviewConfig) bool {
	if cfg.SessionRecall != nil {
		return *cfg.SessionRecall
	}
	return true
}

func (p *backgroundReviewProvider) sessionRecall(ctx context.Context, messages []agentkit.ModelMessage) string {
	if !sessionRecallEnabled(p.cfg) {
		slog.Debug("session recall skipped", "reason", "disabled in config")
		return ""
	}
	if p.index == nil {
		slog.Debug("session recall skipped", "reason", "no session index dep")
		return ""
	}
	if p.learning == nil {
		slog.Debug("session recall skipped", "reason", "no learning dep")
		return ""
	}
	query := reviewRecallQuery(messages)
	if query == "" {
		slog.Debug("session recall skipped", "reason", "no recall query from messages")
		return ""
	}
	dir, err := p.learning.Workspace().Resolve(ctx, p.learning.SessionsDir())
	if err != nil {
		slog.Debug("session recall skipped", "reason", "resolve sessions dir", "err", err)
		return ""
	}
	hits, err := session.SearchSyncedSessions(ctx, p.index, dir, query, 5)
	if err != nil {
		slog.Debug("session recall skipped", "reason", "fts search", "err", err)
		return ""
	}
	return session.FormatSessionRecall(hits)
}

func reviewRecallQuery(messages []agentkit.ModelMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		text := strings.TrimSpace(session.FlattenTextParts(messages[i].Content, " "))
		if text == "" || strings.HasPrefix(text, "/") {
			continue
		}
		return rtlearning.TruncateEllipsis(text, 160)
	}
	return ""
}
