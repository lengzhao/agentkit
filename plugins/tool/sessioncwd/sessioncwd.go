// Package sessioncwd exposes the shared session cwd binding to agents.
package sessioncwd

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	"github.com/lengzhao/pluginkit"
)

type Config struct{}

type Deps struct {
	SessionStore agentkit.SessionStore `json:"sessionStore"`
	Workspace    workspace.Service     `json:"workspace"`
}

type Input struct {
	// Pointer distinguishes explicit reset from a missing or null path.
	Path *string `json:"path" jsonschema:"Existing directory to bind to this session; an empty string clears the session override. Required."`
}

type Output struct {
	CurrentPath string `json:"currentPath"`
	Path        string `json:"path"`
	Note        string `json:"note"`
}

type provider struct{ deps Deps }

func init() { pluginkit.Register("tool/session-cwd", New) }

// New registers tool/session-cwd: set or clear the calling session's cwd.
//
// Best practices:
//   - Mount through tools/runtime deps.dynamicTools to refresh the current cwd in the description each turn.
//   - The same provider can be supplied to acp/session-mcp-tools; calls use their inbound session context.
//   - Changes take effect next turn; an empty path clears only the session override.
func New(_ Config, deps Deps) (agentkit.ToolProvider, error) {
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("tool/session-cwd requires sessionStore")
	}
	if _, ok := deps.SessionStore.(agentkit.SessionCwdStore); !ok {
		return nil, fmt.Errorf("tool/session-cwd requires a session store supporting cwd bindings")
	}
	if deps.Workspace == nil {
		return nil, fmt.Errorf("tool/session-cwd requires workspace")
	}
	return &provider{deps: deps}, nil
}

func (p *provider) ListTools(ctx context.Context) ([]agentkit.Tool, error) {
	current := workspace.WorkingDirectory(ctx)
	if current == "" {
		current = "unbound (agent/tool defaults apply)"
	}
	tool, err := agentkit.NewTool[Input, Output]("session_cwd", p.call).
		Description(fmt.Sprintf("Set this session's working directory. Current turn cwd: %q. Pass path=\"\" to clear the session override and restore inherited defaults. Changes apply on the next turn; the current turn stays in its current directory. A directory change starts a fresh model context while retaining the audit log.", current)).
		Sequential().Build()
	if err != nil {
		return nil, err
	}
	return []agentkit.Tool{tool}, nil
}

func (p *provider) call(ctx context.Context, input Input) (Output, error) {
	if input.Path == nil {
		return Output{}, fmt.Errorf("session_cwd requires path; use an empty string to reset")
	}
	sid := rctx.SessionIDFromContext(ctx)
	if sid == "" {
		return Output{}, fmt.Errorf("session_cwd requires a session")
	}
	path := *input.Path
	var err error
	if path != "" {
		path, err = sessbind.ValidateCwd(ctx, p.deps.Workspace, path)
		if err != nil {
			return Output{}, err
		}
	}
	stack := sessbind.StackFromContext(ctx, p.deps.Workspace).WithSession(sid)
	if err := stack.Set(ctx, p.deps.SessionStore, sessbind.CwdBinding{}, sessbind.LevelSession, path); err != nil {
		return Output{}, err
	}
	return Output{CurrentPath: workspace.WorkingDirectory(ctx), Path: path, Note: "session cwd updated; applies on the next turn; current turn unchanged"}, nil
}
