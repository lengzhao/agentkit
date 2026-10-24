package acpremote

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	capacp "github.com/lengzhao/agentkit/cap/acp"
	"github.com/lengzhao/agentkit/cap/filesystem"
	capsandbox "github.com/lengzhao/agentkit/cap/sandbox"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/acpclient"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type sessionUpdateConsumer interface {
	consume(acp.SessionNotification) error
	finalize() error
}

type turnState struct {
	ctx       context.Context
	emitter   sessionUpdateConsumer
	sessionID agentkit.SessionID
	agentID   agentkit.AgentID
}

type acpPromptResponse struct {
	stopReason string
}

type bridge struct {
	cfg            Config
	workspace      workspace.Service
	fs             filesystem.Service
	sessionMCP     capacp.SessionMCPProvider
	sessionRestore capacp.SessionRestorer
	telemetry      captelemetry.Toolkit
	sandbox        capsandbox.Service

	globalDefaults *globalConfigStore
	connOps        chan connOp
	// proc is the current subprocess snapshot. It is written exclusively by
	// connLoop (via ensureConnOp / subprocessExitOp / detachReleaseOp) and read
	// by callers from any goroutine (cancel, prompt). A Load may
	// return nil or a stale proc that is about to be replaced; readers treat nil
	// as "needs reconnect" on the next prompt. This is best-effort: correctness
	// of prompt/cancel relies on connLoop serializing the real connection state.
	proc      atomic.Pointer[subprocess]
	turn      atomic.Pointer[turnState]
	sessionMu requestMutex // serialize session initialization with slash commands
	configMu  requestMutex // serialize config requests and their full-state responses
}

type subprocess struct {
	cmd               *exec.Cmd
	conn              *acp.ClientSideConnection
	client            *bridgeClient
	steeringSupported bool
	authenticated     bool
	agentCapabilities acp.AgentCapabilities
	done              chan struct{}                 // closed after cmd.Wait returns
	session           atomic.Pointer[remoteSession] // one immutable binding per subprocess
	config            sessionConfigState            // each bridge owns one harness/ACP session
	pendingReplay     bool                          // guarded by bridge.sessionMu; deferred until a turn
}

func (proc *subprocess) alive() bool {
	if proc == nil || proc.done == nil {
		return false
	}
	select {
	case <-proc.done:
		return false
	default:
		return true
	}
}

func newBridge(cfg Config, ws workspace.Service, fs filesystem.Service, sessionMCP capacp.SessionMCPProvider, sessionRestore capacp.SessionRestorer, telemetry captelemetry.Toolkit, sandbox capsandbox.Service, _ func(context.Context) error) *bridge {
	b := &bridge{
		cfg:            cfg,
		workspace:      ws,
		fs:             fs,
		sessionMCP:     sessionMCP,
		sessionRestore: sessionRestore,
		telemetry:      telemetry,
		sandbox:        sandbox,
		connOps:        make(chan connOp),
	}
	go b.connLoop()
	return b
}

func (b *bridge) resolveSessionMCP(ctx context.Context) ([]acp.McpServer, error) {
	if b.sessionMCP == nil {
		// Cursor ACP rejects JSON null for mcpServers; must be an array.
		return acpclient.ToMCPServers(nil), nil
	}
	specs, err := b.sessionMCP.SessionMCPServers(ctx)
	if err != nil {
		return nil, err
	}
	return acpclient.ToMCPServers(specs), nil
}

func (b *bridge) recordSessionMCP(ctx context.Context, servers []acp.McpServer) {
	names := acpclient.MCPServerNames(servers)
	if len(names) == 0 || b.telemetry == nil {
		return
	}
	slog.Info("acp-remote: session mcp servers", "count", len(names), "servers", names)
	b.telemetry.RecordEvent(ctx, "acp.session_mcp", map[string]string{
		"count":   fmt.Sprintf("%d", len(names)),
		"servers": strings.Join(names, ","),
	})
}

func (b *bridge) setTurn(state turnState) {
	st := new(turnState)
	*st = state
	b.turn.Store(st)
}

func (b *bridge) clearTurn() {
	b.turn.Store(nil)
}

func (b *bridge) currentTurn() *turnState {
	return b.turn.Load()
}

// bindDir is the fs-relative dir holding ACP session resume binds.
func (b *bridge) bindDir(context.Context) (string, error) {
	return defaultBindDir, nil
}

// A bridge owns one harness session; reconnecting creates a new subprocess binding.
type remoteSession struct {
	local  agentkit.SessionID
	remote acp.SessionId
}

func (proc *subprocess) trackSession(sessionID agentkit.SessionID, acpSessionID acp.SessionId) {
	proc.session.Store(&remoteSession{local: sessionID, remote: acpSessionID})
}

func terminateAndWaitSubprocess(proc *subprocess, cancel context.CancelFunc) {
	if cancel != nil {
		cancel()
	}
	if proc == nil {
		return
	}
	if proc.cmd != nil {
		terminateProcessGroup(proc.cmd)
	}
	if proc.done != nil {
		<-proc.done
	}
}

func (b *bridge) waitSubprocess(proc *subprocess) {
	err := proc.cmd.Wait()
	if proc.done != nil {
		close(proc.done)
	}
	b.connSend(subprocessExitOp{proc: proc, err: err})
}

// resolveCwd resolves the ACP subprocess working directory and creates it when
// missing, matching tool/shell-bash. Without the mkdir a configured cwd that
// does not exist yet fails the subprocess (chdir) and session/new instead of
// just starting in a fresh directory.
func (b *bridge) resolveCwd(ctx context.Context) (string, error) {
	rel := b.cfg.Cwd
	if rel == "" {
		workDir, _ := workspace.WorkLayout(b.workspace)
		if workDir == "" {
			workDir = "work"
		}
		rel = workDir
	}
	cwd, err := b.workspace.Resolve(ctx, rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		return "", fmt.Errorf("acp-remote: mkdir cwd %s: %w", cwd, err)
	}
	return cwd, nil
}

func (b *bridge) ensureSession(ctx context.Context, sessionID agentkit.SessionID, agentID agentkit.AgentID, sessionStore agentkit.SessionStore, replay bool) (acp.SessionId, error) {
	if err := b.sessionMu.Lock(ctx); err != nil {
		return "", err
	}
	defer b.sessionMu.Unlock()
	proc, err := b.ensureConn(ctx)
	if err != nil {
		return "", err
	}
	if session := proc.session.Load(); session != nil {
		if session.local != sessionID {
			return "", fmt.Errorf("ACP bridge belongs to session %q, not %q", session.local, sessionID)
		}
		if replay && proc.pendingReplay && sessionStore != nil {
			if err := b.replaySessionHistory(ctx, sessionStore, sessionID, session.remote); err != nil {
				return "", err
			}
			proc.pendingReplay = false
		}
		return session.remote, nil
	}
	cwd, err := b.resolveCwd(ctx)
	if err != nil {
		return "", err
	}

	bindDir, err := b.bindDir(ctx)
	if err != nil {
		return "", err
	}
	bindPath, err := acpSessionBindPath(bindDir, sessionID, agentID)
	if err != nil {
		return "", err
	}

	mcpServers, err := b.resolveSessionMCP(ctx)
	if err != nil {
		return "", err
	}
	b.recordSessionMCP(ctx, mcpServers)

	if bind, ok, err := loadACPSessionBind(ctx, b.fs, bindPath); err != nil {
		return "", err
	} else if ok && bind.Cwd == cwd {
		restorer := b.sessionRestore
		if restorer == nil {
			restorer = acpclient.NewRestorer(acpclient.StrategyResume)
		}
		restored, err := restorer.Restore(ctx, configRestoreConn{conn: proc.conn, proc: proc}, capacp.SessionRestoreParams{
			HarnessSessionID:  sessionID,
			AgentID:           agentID,
			ACPSessionID:      bind.ACPSessionID,
			Cwd:               cwd,
			McpServers:        mcpServers,
			AgentCapabilities: proc.agentCapabilities,
		})
		if err != nil {
			wrapped := wrapACPStageError("session/restore", err)
			logACPRemoteError(agentID, b.cfg, sessionID, "session/restore", wrapped)
			return "", wrapped
		}
		if restored {
			proc.trackSession(sessionID, bind.ACPSessionID)
			return bind.ACPSessionID, nil
		}
	}

	defaults, err := b.globalDefaults.load(ctx)
	if err != nil {
		return "", err
	}
	resp, err := proc.conn.NewSession(ctx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: mcpServers,
	})
	if err != nil {
		wrapped := wrapACPStageError("session/new", err)
		logACPRemoteError(agentID, b.cfg, sessionID, "session/new", wrapped)
		return "", wrapped
	}
	proc.updateConfig(resp.ConfigOptions)
	if _, err := applyGlobalConfig(ctx, proc, resp.SessionId, resp.ConfigOptions, defaults); err != nil {
		return "", err
	}
	proc.trackSession(sessionID, resp.SessionId)
	if err := saveACPSessionBind(ctx, b.fs, bindPath, acpSessionBind{
		AgentID:      agentID,
		ACPSessionID: resp.SessionId,
		Cwd:          cwd,
	}); err != nil {
		slog.Debug("acp-remote: save session bind failed", "err", err)
	}
	if sessionStore != nil {
		if replay {
			if err := b.replaySessionHistory(ctx, sessionStore, sessionID, resp.SessionId); err != nil {
				return "", err
			}
		} else {
			proc.pendingReplay = true
		}
	}
	return resp.SessionId, nil
}

func (b *bridge) replaySessionHistory(ctx context.Context, store agentkit.SessionStore, sessionID agentkit.SessionID, remote acp.SessionId) error {
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	if err := b.replayHistory(ctx, sess, remote); err != nil {
		slog.Warn("acp-remote: history replay failed", "session_id", sessionID, "err", err)
	}
	return nil
}

func (b *bridge) prompt(ctx context.Context, acpSessionID acp.SessionId, prompt []acp.ContentBlock) (acpPromptResponse, error) {
	proc, err := b.ensureConn(ctx)
	if err != nil {
		return acpPromptResponse{}, err
	}
	sessionID := rctx.SessionIDFromContext(ctx)
	slog.Info("acp-remote: prompt start", "session_id", sessionID, "acp_session_id", acpSessionID)
	start := time.Now()
	resp, err := proc.conn.Prompt(ctx, acp.PromptRequest{
		SessionId: acpSessionID,
		Prompt:    prompt,
	})
	if err != nil {
		wrapped := wrapACPStageError("session/prompt", err)
		logACPRemoteError(b.cfg.ID, b.cfg, sessionID, "session/prompt", wrapped)
		slog.Warn("acp-remote: prompt failed", "session_id", sessionID, "acp_session_id", acpSessionID, "duration", time.Since(start), "detail", acpErrorDetail(wrapped), "err", wrapped)
		return acpPromptResponse{}, wrapped
	}
	slog.Info("acp-remote: prompt done", "session_id", sessionID, "acp_session_id", acpSessionID, "duration", time.Since(start), "stop_reason", resp.StopReason)
	return acpPromptResponse{stopReason: string(resp.StopReason)}, nil
}

func (b *bridge) cancel(ctx context.Context, acpSessionID acp.SessionId) error {
	proc := b.proc.Load()
	if proc == nil {
		return nil
	}
	return proc.conn.Cancel(ctx, acp.CancelNotification{SessionId: acpSessionID})
}

// bridgeClient implements acp.Client, delegating fs/permission to AgentKit capabilities.
type bridgeClient struct {
	bridge *bridge
}

var _ acp.Client = (*bridgeClient)(nil)

func (c *bridgeClient) SessionUpdate(ctx context.Context, params acp.SessionNotification) error {
	if update := params.Update.ConfigOptionUpdate; update != nil {
		proc := c.bridge.proc.Load()
		if proc == nil || proc.client != c {
			return nil
		}
		session := proc.session.Load()
		if session == nil || session.remote != params.SessionId {
			return nil
		}
		proc.updateConfig(update.ConfigOptions)
		return nil
	}
	turn := c.bridge.currentTurn()
	if turn == nil || turn.emitter == nil {
		return nil
	}
	return turn.emitter.consume(params)
}

func (c *bridgeClient) ReadTextFile(ctx context.Context, params acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	content, err := readTextFile(params.Path, params.Line, params.Limit)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	return acp.ReadTextFileResponse{Content: content}, nil
}

func (c *bridgeClient) WriteTextFile(ctx context.Context, params acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	if err := writeTextFile(params.Path, params.Content); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	return acp.WriteTextFileResponse{}, nil
}

func (c *bridgeClient) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	turn := c.bridge.currentTurn()
	if turn == nil {
		return denyPermission(params), nil
	}
	if c.bridge.cfg.AutoApprove {
		return autoApprovePermission(params), nil
	}
	return requestPermissionViaBroker(turn.ctx, params)
}

func (c *bridgeClient) CreateTerminal(ctx context.Context, params acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, fmt.Errorf("terminal not implemented")
}

func (c *bridgeClient) TerminalOutput(ctx context.Context, params acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, fmt.Errorf("terminal not implemented")
}

func (c *bridgeClient) ReleaseTerminal(ctx context.Context, params acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, nil
}

func (c *bridgeClient) WaitForTerminalExit(ctx context.Context, params acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, fmt.Errorf("terminal not implemented")
}

func (c *bridgeClient) KillTerminal(ctx context.Context, params acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, nil
}

// discard implements io.Writer for unused streams.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

var _ io.Writer = discard{}
