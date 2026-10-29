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
	"github.com/lengzhao/agentkit/runtime/session/sessctx"
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
	// Pool chooses the connection-lifetime policy. Defaults to ttl: connections
	// stay warm (a cold CLI start costs seconds) but idle ones are reclaimed and
	// the cache is capped. Hosts that mint a new session id per turn - a
	// stateless scheduler - should set strategy none so nothing is cached at all.
	Pool PoolConfig `json:"pool,omitempty"`
}

// Deps holds injected capabilities for the ACP client side.
type Deps struct {
	Workspace workspace.Service `json:"workspace"`
	// FS stores session resume binds (tenant-relative acp/) and global ACP defaults.
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
	pool           *bridgePool
	configBridge   *bridge
	configBridgeMu requestMutex
	processCtx     context.Context
	processCancel  context.CancelFunc
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
	processCtx, processCancel := context.WithCancel(context.Background())
	newBridge := func() *bridge {
		b := newBridge(cfg, deps.Workspace, deps.FS, deps.SessionMCP, deps.SessionRestore, deps.Telemetry, sandbox, nil)
		b.globalDefaults = &globalConfigStore{fs: deps.FS, agentID: id}
		return b
	}
	pool, err := newBridgePool(cfg.Pool, newBridge)
	if err != nil {
		processCancel()
		return nil, err
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
		pool:           pool,
		processCtx:     processCtx,
		processCancel:  processCancel,
	}, nil
}

// Slash-owned subprocesses outlive HTTP requests and are cancelled on shutdown.
// Cleanup waits in separate goroutines because shutdown contributions must not block.
func (a *Runtime) ShutdownHooks() []func() {
	return []func(){func() {
		if a.processCancel != nil {
			a.processCancel()
		}
		if a.pool != nil {
			a.pool.Close()
		}
		if err := a.configBridgeMu.Lock(context.Background()); err == nil {
			b := a.configBridge
			a.configBridgeMu.Unlock()
			if b != nil {
				go b.releaseSubprocess()
			}
		}
	}}
}

// acquireLease keeps a session bridge alive while it is in use. Every caller -
// turns and slash commands alike - goes through a lease, so the pool can never
// reclaim a connection someone is using.
func (a *Runtime) acquireLease(ctx context.Context, sessionID agentkit.SessionID) (*bridgeLease, error) {
	if a.pool == nil {
		return nil, fmt.Errorf("acp-remote: bridge pool is not initialized")
	}
	return a.pool.Acquire(ctx, sessionID)
}

// PoolStats reports connection-pool state for diagnostics.
func (a *Runtime) PoolStats() poolStats {
	if a.pool == nil {
		return poolStats{}
	}
	return a.pool.Stats()
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
	lease, err := a.acquireLease(ctx, sessionID)
	if err != nil {
		return err
	}
	// Held for the whole turn: the connection cannot be reclaimed while the
	// turn is running, without any "is a turn in flight" heuristic.
	brid := lease.Bridge()
	defer func() {
		if a.cfg.ReleaseSubprocessAfterTurn {
			brid.releaseSubprocess()
		}
		lease.Release()
	}()
	if err := brid.contextMu.Lock(ctx); err != nil {
		return err
	}
	initializing := true
	defer func() {
		if initializing {
			brid.contextMu.Unlock()
		}
	}()
	var prepareErr error
	ctx, prepareErr = sessctx.Prepare(ctx, a.sessionStore, a.workspace)
	if prepareErr != nil {
		return prepareErr
	}
	// Remote turns must register with the same cancellation control as local steps.
	if ctrl, ok := ctx.Value(agentkit.KeySessionControl).(interface {
		BeginStep(context.Context) (context.Context, func())
		PopCancelReason() string
	}); ok {
		// Keep the reusable subprocess tied to the runner lifetime, while
		// prompt/session requests use the cancellable turn context below.
		ctx = context.WithValue(ctx, subprocessContextKey{}, ctx)
		var endStep func()
		ctx, endStep = ctrl.BeginStep(ctx)
		defer endStep()
		// A /stop can arrive after Loop marks the session busy but before this
		// turn registers a step. Cancel() then has no step to cancel, so the
		// pending reason would be silently dropped by ClearTurnCancel and the
		// subprocess would start for a turn the user already stopped.
		//
		// Popping the reason instead honours that request. It still clears
		// whatever was pending (PopCancelReason consumes), so this keeps the
		// reason from leaking further, but it treats a pending reason at turn
		// start as "stop", not as "stale": a reason left over by a turn that
		// already ended will also cancel this one. That matches what callers
		// work around today (see e.g. /stop landing between turns), and local
		// steps already honour a step-boundary reason the same way.
		if ctrl.PopCancelReason() != "" {
			return context.Canceled
		}
	}
	var finishMCP func() error
	// Bind on every turn, before ensureSession's reused-session fast path.
	if lifecycle, ok := brid.sessionMCP.(capacp.SessionMCPTurnProvider); ok {
		if ctx.Value(subprocessContextKey{}) == nil {
			ctx = context.WithValue(ctx, subprocessContextKey{}, a.processCtx)
		}
		ctx = capacp.WithSessionMCPBinding(ctx, brid.mcpBinding)
		brid.rememberMCPCleanup(ctx)
		nextCtx, endMCPTurn, beginErr := lifecycle.BeginMCPTurn(ctx)
		if beginErr != nil {
			return beginErr
		}
		if nextCtx == nil || endMCPTurn == nil {
			return fmt.Errorf("session MCP lifecycle returned an invalid turn binding")
		}
		ctx = nextCtx
		finishMCP = sync.OnceValue(endMCPTurn)
		defer func() { _ = finishMCP() }()
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
	}()

	if finishMCP != nil {
		defer func() {
			// Freeze the outcome before resource cleanup cancels the tool context.
			_ = endData()
			if err := finishMCP(); err != nil {
				turnErr = err
			}
		}()
	}
	acpSessionID, err := brid.ensureSession(ctx, sessionID, a.id, a.sessionStore, true)
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
	brid.contextMu.Unlock()
	initializing = false

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
				steeringErr := forwardSteering(ctx, stop, ctrl, proc.conn, acpSessionID, brid.releaseSubprocess, func(msg agentkit.ModelMessage) error {
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
				logACPRemoteError(a.id, a.cfg, sessionID, "_session/steering", steeringErr)
				steeringDone <- steeringErr
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
