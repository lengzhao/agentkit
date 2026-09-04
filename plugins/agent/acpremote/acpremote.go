package acpremote

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/pluginkit"
)

// Config configures a remote ACP agent subprocess (Claude Code, Cursor CLI, etc.).
type Config struct {
	// ID is the agent id referenced by loop.defaultAgent.
	ID agentkit.AgentID `json:"id"`
	// Command is the subprocess to spawn, e.g. ["agent", "acp"] (Cursor CLI) or
	// ["npx", "-y", "@zed-industries/claude-code-acp@latest"].
	Command []string `json:"command"`
	// Env adds environment variables for the subprocess.
	Env map[string]string `json:"env,omitempty"`
	// Cwd is the working directory for ACP session/new. Empty uses workspace root.
	Cwd string `json:"cwd,omitempty"`
	// AutoApprove automatically grants tool permission requests without prompting.
	AutoApprove bool `json:"autoApprove"`
	// AuthMethod is passed to authenticate when non-empty (e.g. "cursor_login").
	AuthMethod string `json:"authMethod,omitempty"`
	// ClientName is sent in initialize; defaults to "agentkit".
	ClientName string `json:"clientName,omitempty"`
	// ClientVersion is sent in initialize; defaults to "0.1.0".
	ClientVersion string `json:"clientVersion,omitempty"`
}

// Deps holds injected capabilities for the ACP client side.
type Deps struct {
	Workspace    workspace.Service      `json:"workspace"`
	SessionStore agentkit.SessionStore `json:"sessionStore,omitempty"`
}

// Runtime proxies turns to an external ACP agent over stdio.
type Runtime struct {
	id           agentkit.AgentID
	cfg          Config
	workspace    workspace.Service
	sessionStore agentkit.SessionStore
	bridge       *bridge
}

func init() {
	pluginkit.Register("agent/acp-remote", New)
}

// New registers agent/acp-remote: run turns via an external ACP agent subprocess.
func New(cfg Config, deps Deps) (agentkit.Agent, error) {
	if len(cfg.Command) == 0 {
		return nil, fmt.Errorf("agent/acp-remote requires command")
	}
	if deps.Workspace == nil {
		return nil, fmt.Errorf("agent/acp-remote requires workspace")
	}
	id := cfg.ID
	if id == "" {
		id = "acp"
	}
	clientName := cfg.ClientName
	if clientName == "" {
		clientName = "agentkit"
	}
	clientVersion := cfg.ClientVersion
	if clientVersion == "" {
		clientVersion = "0.1.0"
	}
	cfg.ClientName = clientName
	cfg.ClientVersion = clientVersion
	return &Runtime{
		id:           id,
		cfg:          cfg,
		workspace:    deps.Workspace,
		sessionStore: deps.SessionStore,
		bridge:       newBridge(cfg, deps.Workspace),
	}, nil
}

func (a *Runtime) ID() agentkit.AgentID { return a.id }

func (a *Runtime) AgentCatalogEntry() string {
	var b strings.Builder
	fmt.Fprintf(&b, "agent %q\n", a.id)
	b.WriteString("kind: agent/acp-remote\n")
	if len(a.cfg.Command) > 0 {
		fmt.Fprintf(&b, "command: %s\n", strings.Join(a.cfg.Command, " "))
	}
	if a.cfg.Cwd != "" {
		fmt.Fprintf(&b, "cwd: %s\n", a.cfg.Cwd)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (a *Runtime) RunTurn(ctx context.Context, input agentkit.TurnInput) error {
	sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	if sessionID == "" {
		return fmt.Errorf("turn requires session id in context")
	}
	emit := input.Emit
	if emit == nil {
		return fmt.Errorf("turn requires outbound emit")
	}

	if a.sessionStore != nil {
		sess, err := a.sessionStore.Get(ctx, sessionID)
		if err != nil {
			return err
		}
		if err := session.AppendTurnStart(ctx, sess, a.id); err != nil {
			return err
		}
		defer func() {
			endCtx := context.WithoutCancel(ctx)
			_ = session.AppendTurnEnd(endCtx, sess, a.id, 1)
		}()
		if err := session.AppendMessage(ctx, sess, a.id, agentkit.EventUserMessage, input.Message); err != nil {
			return err
		}
	}

	if err := a.emitLifecycle(ctx, emit, sessionID, agentkit.EventTurnStart, session.TurnStartData{}); err != nil {
		return err
	}
	defer func() {
		endCtx := context.WithoutCancel(ctx)
		_ = a.emitLifecycle(endCtx, emit, sessionID, agentkit.EventTurnEnd, session.TurnEndData{Steps: 1})
	}()

	acpSessionID, err := a.ensureACPSessionWithAuth(ctx, emit, sessionID)
	if err != nil {
		return err
	}

	emitter := newUpdateEmitter(ctx, sessionID, a.id, emit)
	a.bridge.setTurn(turnState{
		ctx:       ctx,
		emitter:   emitter,
		sessionID: sessionID,
		agentID:   a.id,
	})
	defer a.bridge.clearTurn()

	prompt := modelMessageToPrompt(input.Message)
	if len(prompt) == 0 {
		return fmt.Errorf("empty user message")
	}

	type promptResult struct {
		resp acpPromptResponse
		err  error
	}
	done := make(chan promptResult, 1)
	go func() {
		resp, err := a.bridge.prompt(ctx, acpSessionID, prompt)
		done <- promptResult{resp: resp, err: err}
	}()

	select {
	case <-ctx.Done():
		_ = a.bridge.cancel(context.Background(), acpSessionID)
		return ctx.Err()
	case result := <-done:
		if result.err != nil {
			return result.err
		}
		if err := emitter.finalize(); err != nil {
			return err
		}
		if a.sessionStore != nil {
			sess, err := a.sessionStore.Get(ctx, sessionID)
			if err != nil {
				return err
			}
			assistant := emitter.assistantMessage()
			if assistant.Role != "" {
				if err := session.AppendMessage(ctx, sess, a.id, agentkit.EventAssistantMessage, assistant); err != nil {
					slog.Debug("acp-remote: append assistant message failed", "err", err)
				}
			}
		}
		if result.resp.stopReason == "cancelled" {
			return context.Canceled
		}
		return nil
	}
}

func (a *Runtime) emitLifecycle(ctx context.Context, emit agentkit.OutboundEmit, sessionID agentkit.SessionID, typ agentkit.EventType, payload any) error {
	return emit(ctx, agentkit.OutboundEvent{
		SessionID: sessionID,
		AgentID:   a.id,
		Type:      typ,
		Data:      agentkit.MarshalOutboundData(payload),
	})
}
