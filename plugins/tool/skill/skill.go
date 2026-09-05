package skill

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/skill"
	"github.com/lengzhao/agentkit/runtime/session"
)

type SkillConfig struct{}

type SkillDeps struct {
	Skills       skill.Registry        `json:"skills"`
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

type SkillInput struct {
	Name string `json:"name" jsonschema:"Skill name to load"`
}

// NewSkill registers tool/skill: Load an agent skill by name and inject SKILL.md.
//
// Best practices:
//   - Load a skill once per task, then follow its instructions.
//   - Read supporting files with fs tools (tenant skills live under skills/<name>/).
//   - Run bundled scripts with bash from the skill directory named in the load result.
func NewSkill(_ SkillConfig, deps SkillDeps) (agentkit.Tool, error) {
	if deps.Skills == nil {
		return nil, fmt.Errorf("tool/skill requires skills dependency")
	}
	store := deps.SessionStore
	tool, err := agentkit.NewTool[SkillInput, string]("skill", func(ctx context.Context, input SkillInput) (string, error) {
		name := strings.TrimSpace(input.Name)
		if name == "" {
			return "", fmt.Errorf("skill name is required")
		}
		content, err := deps.Skills.Load(ctx, name)
		if err != nil {
			return "", err
		}
		sessionID := session.SessionIDFromContext(ctx)
		agentID := session.AgentIDFromContext(ctx)
		if sessionID != "" {
			if store == nil {
				return "", fmt.Errorf("tool/skill requires sessionStore dependency")
			}
			sess, err := store.Get(ctx, sessionID)
			if err != nil {
				return "", err
			}
			if err := session.AppendSkillLoad(ctx, sess, agentID, content); err != nil {
				return "", err
			}
		}
		return skill.RenderLoaded(content), nil
	}).Description("Load a skill by name and inject its SKILL.md instructions into the session. Read supporting files with read; run bundled scripts with bash.").Build()
	if err != nil {
		return nil, err
	}
	return tool, nil
}
