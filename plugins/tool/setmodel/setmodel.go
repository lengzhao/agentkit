package setmodel

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// Config configures tool/set-model.
type Config struct {
	// AllowModels is the whitelist of model ids the agent may switch to.
	// Empty means no restriction.
	AllowModels []string `json:"allowModels,omitempty"`
}

// Deps holds injected capabilities for tool/set-model.
type Deps struct {
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

// Input is the set_model tool call payload.
type Input struct {
	Model string `json:"model" jsonschema:"Model id to use for this session from now on; use reset to clear the session override"`
}

// Output is the set_model tool result.
type Output struct {
	Model string `json:"model"`
	Reset bool   `json:"reset,omitempty"`
	Note  string `json:"note"`
}

// New registers tool/set-model: let the agent switch this session's LLM model (tool name: set_model).
//
// Best practices:
//   - Writes the same session runtime.json model override as /model; the new model takes effect on the next turn.
//   - Configure allowModels in production so the agent can only pick deployed models.
//   - Subagent contexts are denied: children keep their own definition model.
func New(cfg Config, deps Deps) (agentkit.Tool, error) {
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("tool/set-model requires sessionStore dependency")
	}
	store, ok := deps.SessionStore.(agentkit.SessionRuntimeStore)
	if !ok {
		return nil, fmt.Errorf("tool/set-model requires a session store with runtime override support")
	}
	allow := make(map[string]bool, len(cfg.AllowModels))
	for _, m := range cfg.AllowModels {
		if m = strings.TrimSpace(m); m != "" {
			allow[m] = true
		}
	}
	tool, err := agentkit.NewTool[Input, Output]("set_model", func(ctx context.Context, input Input) (Output, error) {
		if ctx.Value(agentkit.KeyInSubagent) != nil {
			return Output{}, fmt.Errorf("set_model is not available inside a subagent")
		}
		sessionID := rctx.SessionIDFromContext(ctx)
		if sessionID == "" {
			return Output{}, fmt.Errorf("set_model requires a session")
		}
		model := strings.TrimSpace(input.Model)
		if model == "" {
			return Output{}, fmt.Errorf("set_model requires a model id (or reset)")
		}
		if strings.EqualFold(model, "reset") || strings.EqualFold(model, "default") {
			if err := store.SetModelBind(ctx, sessionID, ""); err != nil {
				return Output{}, err
			}
			return Output{Reset: true, Note: "session model override cleared; the agent default applies from the next turn"}, nil
		}
		if len(allow) > 0 && !allow[model] {
			return Output{}, fmt.Errorf("model %q is not in allowModels", model)
		}
		if err := store.SetModelBind(ctx, sessionID, model); err != nil {
			return Output{}, err
		}
		return Output{Model: model, Note: "session model set; it takes effect from the next turn"}, nil
	}).
		Description("Switch the LLM model used for this session, persistently. " +
			"Use when the task at hand clearly calls for a different model (e.g. a stronger one for hard reasoning, a cheaper one for simple edits). " +
			"The change applies from the next turn, not mid-turn. Use model=reset to clear the override.").
		Build()
	if err != nil {
		return nil, err
	}
	return tool, nil
}
