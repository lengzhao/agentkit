package acpremote

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	capacp "github.com/lengzhao/agentkit/cap/acp"
	"github.com/lengzhao/agentkit/cap/filesystem"
	capsandbox "github.com/lengzhao/agentkit/cap/sandbox"
	capsession "github.com/lengzhao/agentkit/cap/session"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
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
	// Cwd is the working directory for the ACP subprocess (cmd.Dir) and session/new.
	// Empty uses workspace work/ (same default as tool/shell-bash). Any value the
	// workspace resolves is accepted — absolute paths and ~/ pass through
	// unchecked — and a missing directory is created (like tool/shell-bash).
	Cwd string `json:"cwd,omitempty"`
	// ReleaseSubprocessAfterTurn kills the ACP process group after each RunTurn so
	// Cursor child processes (language servers, worker-server) do not accumulate.
	ReleaseSubprocessAfterTurn bool `json:"releaseSubprocessAfterTurn,omitempty"`
	// AutoApprove automatically grants tool permission requests without prompting.
	AutoApprove bool `json:"autoApprove"`
	// AuthMethod is passed to authenticate when non-empty (e.g. "cursor_login").
	AuthMethod string `json:"authMethod,omitempty"`
	// ClientName is sent in initialize; defaults to "agentkit".
	ClientName string `json:"clientName,omitempty"`
	// ClientVersion is sent in initialize; defaults to "0.1.0".
	ClientVersion string `json:"clientVersion,omitempty"`
	// AuthHint overrides the user-facing guidance when ACP requires authentication,
	// during connection, session creation, or a prompt. Secret values are never included.
	AuthHint string `json:"authHint,omitempty"`
}

// Deps holds injected capabilities for the ACP client side.
type Deps struct {
	Workspace workspace.Service `json:"workspace"`
	// FS stores ACP session resume binds (tenant-relative acp/ dir).
	FS            filesystem.Service      `json:"fs"`
	SessionStore  agentkit.SessionStore   `json:"sessionStore,omitempty"`
	SessionEvents capsession.Conversation `json:"sessionEvents,omitempty"`
	// SessionMCP supplies harness MCP servers for session/new (not project mcp.json).
	SessionMCP capacp.SessionMCPProvider `json:"sessionMcp,omitempty"`
	// SessionRestore customizes session/resume vs session/load before session/new fallback.
	SessionRestore capacp.SessionRestorer `json:"sessionRestore,omitempty"`
	Telemetry      captelemetry.Toolkit   `json:"telemetry"`
	// Sandbox wraps the ACP subprocess command with the tenant sandbox view
	// (e.g. sandbox/bwrap) at spawn time. Nil defaults to
	// capsandbox.Disabled() (passthrough).
	Sandbox capsandbox.Service `json:"sandbox,omitempty"`
}

// Runtime proxies turns to an external ACP agent over stdio.
type Runtime struct {
	id             agentkit.AgentID
	cfg            Config
	workspace      workspace.Service
	fs             filesystem.Service
	sessionStore   agentkit.SessionStore
	sessionEvents  capsession.Conversation
	sessionMCP     capacp.SessionMCPProvider
	sessionRestore capacp.SessionRestorer
	telemetry      captelemetry.Toolkit
	sandbox        capsandbox.Service
	bridges        sync.Map // agentkit.SessionID -> *bridge
}

func init() {
	pluginkit.Register("agent/acp-remote", New)
}

// New registers agent/acp-remote: run turns via an external ACP agent subprocess.
// SetDefaults implements pluginkit.Defaulter.
func (c *Config) SetDefaults() {
	if c.ID == "" {
		c.ID = "acp"
	}
	if c.ClientName == "" {
		c.ClientName = "agentkit"
	}
	if c.ClientVersion == "" {
		c.ClientVersion = "0.1.0"
	}
}

// Validate implements pluginkit.Validator.
func (c *Config) Validate() error {
	if len(c.Command) == 0 {
		return fmt.Errorf("agent/acp-remote requires command")
	}
	return nil
}

func New(cfg Config, deps Deps) (agentkit.Agent, error) {
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if deps.Workspace == nil {
		return nil, fmt.Errorf("agent/acp-remote requires workspace")
	}
	if deps.FS == nil {
		return nil, fmt.Errorf("agent/acp-remote requires fs")
	}
	if deps.Telemetry == nil {
		return nil, fmt.Errorf("agent/acp-remote requires telemetry dependency")
	}
	id := cfg.ID
	sandbox := deps.Sandbox
	if sandbox == nil {
		sandbox = capsandbox.Disabled()
	}
	return &Runtime{
		id:             id,
		cfg:            cfg,
		workspace:      deps.Workspace,
		fs:             deps.FS,
		sessionStore:   deps.SessionStore,
		sessionEvents:  deps.SessionEvents,
		sessionMCP:     deps.SessionMCP,
		sessionRestore: deps.SessionRestore,
		telemetry:      deps.Telemetry,
		sandbox:        sandbox,
	}, nil
}

func (a *Runtime) bridgeFor(sessionID agentkit.SessionID) *bridge {
	v, _ := a.bridges.LoadOrStore(sessionID, newBridge(a.cfg, a.workspace, a.fs, a.sessionMCP, a.sessionRestore, a.telemetry, a.sandbox, nil))
	return v.(*bridge)
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

func (a *Runtime) RunTurn(ctx context.Context, input agentkit.TurnInput) (turnErr error) {
	// Handle authentication errors from every stage, including session/prompt.
	// The runner displays this error once; preserve its cause for inspection.
	defer func() { turnErr = a.withAuthGuidance(ctx, turnErr) }()
	sessionID := rctx.SessionIDFromContext(ctx)
	if sessionID == "" {
		return fmt.Errorf("turn requires session id in context")
	}
	emit := input.Emit
	if emit == nil {
		return fmt.Errorf("turn requires outbound emit")
	}
	// Remote turns must register with the same cancellation control as local steps.
	if ctrl, ok := ctx.Value(agentkit.KeySessionControl).(interface {
		ClearTurnCancel()
		BeginStep(context.Context) (context.Context, func())
	}); ok {
		ctrl.ClearTurnCancel()
		// Keep the reusable subprocess tied to the runner lifetime, while
		// prompt/session requests use the cancellable turn context below.
		ctx = context.WithValue(ctx, subprocessContextKey{}, ctx)
		var endStep func()
		ctx, endStep = ctrl.BeginStep(ctx)
		defer endStep()
	}
	endData := sync.OnceValue(func() capsession.TurnEndData {
		data := capsession.TurnEndData{Steps: 1}
		if errors.Is(turnErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			data.Cancelled = true
			data.StopReason = context.Canceled.Error()
			if ctrl, ok := ctx.Value(agentkit.KeySessionControl).(interface{ PopCancelReason() string }); ok {
				if reason := ctrl.PopCancelReason(); reason != "" {
					data.StopReason = reason
				}
			}
		}
		return data
	})
	// Async wrapping is owned by forwardParentEmit (loop-agent / inprocess),
	// which also holds the AsyncEmitter.Close path. Doing it here would double-
	// wrap and leak a goroutine with no closer.

	if a.sessionStore != nil && a.sessionEvents != nil {
		sess, err := a.sessionStore.Get(ctx, sessionID)
		if err != nil {
			return err
		}
		if err := a.sessionEvents.AppendTurnStart(ctx, sess, a.id); err != nil {
			return err
		}
		defer func() {
			endCtx := context.WithoutCancel(ctx)
			_ = a.sessionEvents.AppendTurnEnd(endCtx, sess, a.id, endData())
		}()
		if err := a.sessionEvents.AppendMessage(ctx, sess, a.id, agentkit.EventUserMessage, input.Message); err != nil {
			return err
		}
	}

	if err := a.emitLifecycle(ctx, emit, agentkit.EventTurnStart, capsession.TurnStartData{}); err != nil {
		return err
	}
	defer func() {
		endCtx := context.WithoutCancel(ctx)
		_ = a.emitLifecycle(endCtx, emit, agentkit.EventTurnEnd, endData())
		if a.cfg.ReleaseSubprocessAfterTurn {
			a.bridgeFor(sessionID).releaseSubprocess()
		}
	}()

	brid := a.bridgeFor(sessionID)
	acpSessionID, err := brid.ensureACPSession(ctx, sessionID, a.id, a.sessionStore)
	if err != nil {

		return err
	}

	emitter := newUpdateEmitter(a.telemetry, ctx, sessionID, a.id, emit, input.Message)
	brid.setTurn(turnState{
		ctx:       ctx,
		emitter:   emitter,
		sessionID: sessionID,
		agentID:   a.id,
	})
	defer brid.clearTurn()

	prompt := modelMessageToPrompt(input.Message)
	if len(prompt) == 0 {
		return fmt.Errorf("empty user message")
	}

	type promptResult struct {
		resp acpPromptResponse
		err  error
	}
	promptCtx, endPrompt := brid.beginPromptContext(ctx)
	defer endPrompt()

	done := make(chan promptResult, 1)
	go func() {
		resp, err := brid.prompt(promptCtx, acpSessionID, prompt)
		done <- promptResult{resp: resp, err: err}
	}()

	// Unsupported peers leave the queue untouched for Loop's next-turn fallback.
	finishSteering := func() error { return nil }
	if ctrl, ok := ctx.Value(agentkit.KeySessionControl).(steeringControl); ok {
		if proc := brid.proc.Load(); proc != nil && proc.steeringSupported {
			stop := make(chan struct{})
			steeringDone := make(chan error, 1)
			go func() {
				steeringDone <- forwardSteering(ctx, stop, ctrl, proc.conn, acpSessionID, brid.releaseSubprocess, func(msg agentkit.ModelMessage) error {
					if a.sessionStore == nil || a.sessionEvents == nil {
						return nil
					}
					// Acknowledged input belongs in history even if the turn was
					// cancelled just as the acknowledgement arrived.
					persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					defer cancel()
					sess, err := a.sessionStore.Get(persistCtx, sessionID)
					if err != nil {
						return err
					}
					return a.sessionEvents.AppendMessage(persistCtx, sess, a.id, agentkit.EventUserMessage, msg)
				})
			}()
			var once sync.Once
			var steeringErr error
			finishSteering = func() error {
				once.Do(func() { close(stop); steeringErr = <-steeringDone })
				return steeringErr
			}
			defer func() {
				if err := finishSteering(); turnErr == nil {
					turnErr = err
				}
			}()
		}
	}

	select {
	case <-ctx.Done():
		_ = brid.cancel(context.Background(), acpSessionID)
		return ctx.Err()
	case result := <-done:
		_ = finishSteering() // Join before finalizing output and persisting the assistant.
		if ctx.Err() != nil {
			_ = brid.cancel(context.Background(), acpSessionID)
			return ctx.Err()
		}
		if result.err != nil {
			return result.err
		}
		if err := emitter.finalize(); err != nil {
			return err
		}
		if a.sessionStore != nil && a.sessionEvents != nil {
			sess, err := a.sessionStore.Get(ctx, sessionID)
			if err != nil {
				return err
			}
			assistant := emitter.assistantMessage()
			if assistant.Role != "" {
				if err := a.sessionEvents.AppendMessage(ctx, sess, a.id, agentkit.EventAssistantMessage, assistant); err != nil {
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

func (a *Runtime) emitLifecycle(ctx context.Context, emit agentkit.OutboundEmit, typ agentkit.EventType, payload any) error {
	return emit(ctx, agentkit.OutboundEvent{
		AgentID: a.id,
		Type:    typ,
		Data:    rctx.MarshalOutboundData(payload),
	})
}
