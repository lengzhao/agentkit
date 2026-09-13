package learning

import (
	"context"

	"github.com/lengzhao/agentkit/cap/workspace"
)

// SkillProposer applies learn_capture skill_propose during background review.
type SkillProposer interface {
	CaptureSkillPropose(ctx context.Context, name, body, sessionID, focus, source string) (string, error)
}

// CaptureInput is the learn_capture tool schema.
type CaptureInput struct {
	Action  string `json:"action" jsonschema:"memory_add | memory_replace | memory_remove | skill_propose"`
	Content string `json:"content,omitempty" jsonschema:"Text for memory_add/replace or skill body for skill_propose"`
	OldText string `json:"old_text,omitempty" jsonschema:"Substring for memory_remove or memory_replace"`
	Name    string `json:"name,omitempty" jsonschema:"Skill name for skill_propose"`
	Focus   string `json:"focus,omitempty" jsonschema:"Short focus for skill_propose"`
}

// CaptureOutput is returned to the review model.
type CaptureOutput struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// NudgeSessionState tracks background memory review cadence for one conversation.
type NudgeSessionState struct {
	TurnsSinceMemory int `json:"turnsSinceMemory"`
}

// ReviewHost is the learning/default surface for hook/background-review (orchestration only).
type ReviewHost interface {
	Disabled() bool
	Workspace() workspace.Service
	SessionsDir() string
	ReviewSignalCandidates(ctx context.Context) string
	TryConsumeReviewQuota(ctx context.Context, maxPerDay int) (bool, error)
	ReviewNudgeLoad(ctx context.Context, sessionID string) (NudgeSessionState, bool, error)
	ReviewNudgeSave(ctx context.Context, sessionID string, state NudgeSessionState) error
}

// DreamSweepScheduler is the learning/default surface for learning/dream-sweep.
type DreamSweepScheduler interface {
	Disabled() bool
	Workspace() workspace.Service
	DreamSweepDue(ctx context.Context) bool
	RunScheduledDreamSweep(ctx context.Context) error
}
