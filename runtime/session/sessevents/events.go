package sessevents

import (
	"context"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/cap/skill"
	"github.com/lengzhao/agentkit/runtime/session/derive"
)

// events is the standard capsession.Events implementation: stateless, all
// conventions applied at call time from the request context. The same value
// satisfies Conversation, RunLog, Compaction, and Skills.
type events struct{}

// Default is the shared stateless instance. Runtime code appends contract
// events through it; runtime-only markers (subagent, session recovery) stay
// package-level functions.
var Default capsession.Events = events{}

// New constructs the session/events capability instance.
func New() (capsession.Events, error) {
	return Default, nil
}

func (events) RenderSkillContent(content skill.Content) string {
	return derive.RenderSkillLoaded(content)
}

func (events) AppendSkillLoad(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, content skill.Content) (string, error) {
	return derive.AppendSkillLoad(ctx, s, agentID, content)
}

func (events) IndexForCompaction(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID) ([]compaction.IndexedMessage, error) {
	all, err := s.Read(ctx, 0)
	if err != nil {
		return nil, err
	}
	return derive.IndexMessagesForCompaction(all, agentID), nil
}
