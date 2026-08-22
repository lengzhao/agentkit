package skilltool

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/skill"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/pluginkit"
)

type Config struct{}

type Deps struct {
	Skills       skill.Registry        `json:"skills"`
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

type Input struct {
	Name string `json:"name" jsonschema:"required,description=Skill name to load"`
}

type Output struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

func init() {
	pluginkit.Register("tool/skill", New)
}

func New(_ Config, deps Deps) (agentkit.Tool, error) {
	if deps.Skills == nil {
		return nil, fmt.Errorf("tool/skill requires skills dependency")
	}
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("tool/skill requires sessionStore dependency")
	}
	store := deps.SessionStore
	return agentkit.NewTool[Input, Output]("skill", func(ctx context.Context, input Input) (Output, error) {
		if input.Name == "" {
			return Output{}, fmt.Errorf("skill name is required")
		}
		content, err := deps.Skills.Load(ctx, input.Name)
		if err != nil {
			return Output{}, err
		}
		sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
		agentID, _ := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)
		if sessionID != "" {
			sess, err := store.Get(ctx, sessionID)
			if err != nil {
				return Output{}, err
			}
			if err := session.AppendSkillLoad(ctx, sess, agentID, content.Name, content.Description, content.Body); err != nil {
				return Output{}, err
			}
		}
		return Output{
			Name:        content.Name,
			Description: content.Description,
			Body:        content.Body,
		}, nil
	}).Description("Load a skill by name and inject its instructions into the session.").Build()
}
