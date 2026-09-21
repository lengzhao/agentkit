package skill

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/cap/skill"
	"github.com/lengzhao/agentkit/runtime/rctx"
	rtskill "github.com/lengzhao/agentkit/runtime/skill"
)

type SkillConfig struct{}

type SkillDeps struct {
	Skills        skill.Registry        `json:"skills"`
	SessionStore  agentkit.SessionStore `json:"sessionStore"`
	SessionEvents capsession.Skills     `json:"sessionEvents"`
}

type SkillInput struct {
	Name string `json:"name" jsonschema:"Skill name to load"`
}

// NewSkill registers tool/skill: Load an agent skill by name and inject SKILL.md.
//
// Best practices:
//   - Load a skill once per task, then follow its instructions.
//   - Read supporting files with fs tools (tenant skills live under skills/<name>/).
//   - Run bundled scripts with bash using the absolute skill directory from the load result.
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
		sessionID := rctx.SessionIDFromContext(ctx)
		agentID := rctx.AgentIDFromContext(ctx)
		if sessionID != "" {
			if store == nil || deps.SessionEvents == nil {
				return "", fmt.Errorf("tool/skill requires sessionStore and sessionEvents dependencies")
			}
			sess, err := store.Get(ctx, sessionID)
			if err != nil {
				return "", err
			}
			if err := deps.SessionEvents.AppendSkillLoad(ctx, sess, agentID, content); err != nil {
				return "", err
			}
		}
		return rtskill.RenderLoaded(content), nil
	}).Description("Load a skill by name and inject its SKILL.md instructions into the session. Use absolute paths with read and bash (skill base directory is absolute in the load result).").Build()
	if err != nil {
		return nil, err
	}
	return tool, nil
}
