package skill

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/skill"
	"github.com/lengzhao/agentkit/runtime/session"
)

type SkillConfig struct {
	// ScriptTimeoutSeconds limits skill script execution; 0 uses the default (60s).
	ScriptTimeoutSeconds int `json:"scriptTimeoutSeconds,omitempty"`
}

type SkillDeps struct {
	Skills       skill.Registry        `json:"skills"`
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

type SkillInput struct {
	Name   string   `json:"name" jsonschema:"Skill name to load"`
	File   string   `json:"file,omitempty" jsonschema:"Optional supporting file relative to the skill directory (for example reference.md)"`
	Script string   `json:"script,omitempty" jsonschema:"Optional script relative to the skill directory (for example scripts/run.sh)"`
	Args   []string `json:"args,omitempty" jsonschema:"Arguments passed to the skill script"`
}

// NewSkill registers tool/skill: Discover and load an agent skill by name.
//
// Best practices:
//   - Load a skill once per task, then follow its instructions.
//   - Use file to read supporting resources named in SKILL.md without opening the whole skills tree to fs tools.
//   - Use script to run scripts bundled under the skill directory.
func NewSkill(cfg SkillConfig, deps SkillDeps) (agentkit.Tool, error) {
	if deps.Skills == nil {
		return nil, fmt.Errorf("tool/skill requires skills dependency")
	}
	timeout := time.Duration(cfg.ScriptTimeoutSeconds) * time.Second
	store := deps.SessionStore
	tool, err := agentkit.NewTool[SkillInput, string]("skill", func(ctx context.Context, input SkillInput) (string, error) {
		name := strings.TrimSpace(input.Name)
		if name == "" {
			return "", fmt.Errorf("skill name is required")
		}
		file := strings.TrimSpace(input.File)
		script := strings.TrimSpace(input.Script)
		if file != "" && script != "" {
			return "", fmt.Errorf("file and script are mutually exclusive")
		}
		if script != "" {
			result, err := skill.RunScript(ctx, deps.Skills, name, script, input.Args, timeout)
			if err != nil {
				return "", err
			}
			return skill.RenderScriptResult(result), nil
		}
		var content skill.Content
		var err error
		if file == "" {
			content, err = deps.Skills.Load(ctx, name)
		} else {
			content, err = skill.ReadFile(ctx, deps.Skills, name, file)
		}
		if err != nil {
			return "", err
		}
		if file == "" {
			sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
			agentID, _ := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)
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
		}
		return skill.RenderResourceFile(content.Name, content.Path, file, content.Body), nil
	}).Description("Load a skill by name and inject its SKILL.md instructions into the session. Use file to read a supporting resource, or script to run a bundled script from the skill directory.").Build()
	if err != nil {
		return nil, err
	}
	return tool, nil
}
