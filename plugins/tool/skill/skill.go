package skill

import (
	"context"
	"fmt"

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
	Name string `json:"name" jsonschema:"required,description=Skill name to load"`
}

type SkillOutput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

// NewSkill registers tool/skill: Discover and load an agent skill by name.
//
// Best practices:
//   - Load a skill once per task, then follow its instructions.
func NewSkill(_ SkillConfig, deps SkillDeps) (agentkit.ToolPack, error) {
	if deps.Skills == nil {
		return nil, fmt.Errorf("tool/skill requires skills dependency")
	}
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("tool/skill requires sessionStore dependency")
	}
	store := deps.SessionStore
	tool, err := agentkit.NewTool[SkillInput, SkillOutput]("skill", func(ctx context.Context, input SkillInput) (SkillOutput, error) {
		if input.Name == "" {
			return SkillOutput{}, fmt.Errorf("skill name is required")
		}
		content, err := deps.Skills.Load(ctx, input.Name)
		if err != nil {
			return SkillOutput{}, err
		}
		sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
		agentID, _ := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)
		if sessionID != "" {
			sess, err := store.Get(ctx, sessionID)
			if err != nil {
				return SkillOutput{}, err
			}
			if err := session.AppendSkillLoad(ctx, sess, agentID, content.Name, content.Description, content.Body); err != nil {
				return SkillOutput{}, err
			}
		}
		return SkillOutput{
			Name:        content.Name,
			Description: content.Description,
			Body:        content.Body,
		}, nil
	}).Description("Load a skill by name and inject its instructions into the session.").Build()
	if err != nil {
		return nil, err
	}
	return agentkit.Pack(tool), nil
}
