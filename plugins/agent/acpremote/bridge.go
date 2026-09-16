package acpremote

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	capacp "github.com/lengzhao/agentkit/cap/acp"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/acpclient"
	rttelemetry "github.com/lengzhao/agentkit/runtime/telemetry"
	"github.com/lengzhao/agentkit/runtime/session"
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
	cfg        Config
	workspace  workspace.Service
	sessionMCP capacp.SessionMCPProvider

	mu           sync.Mutex
	proc         *subprocess
	promptCancel context.CancelFunc
	promptGen    uint64
	turn         *turnState
	turnMu       sync.Mutex
}

type subprocess struct {
	cmd           *exec.Cmd
	conn          *acp.ClientSideConnection
	client        *bridgeClient
	authenticated bool
	done          chan struct{} // closed after cmd.Wait returns
	sessions      sync.Map      // agentkit.SessionID -> acp.SessionId
	acpSessions   sync.Map      // acp.SessionId -> agentkit.SessionID
	states        sync.Map      // agentkit.SessionID -> *sessionState
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

func newBridge(cfg Config, ws workspace.Service, sessionMCP capacp.SessionMCPProvider) *bridge {
	b := &bridge{cfg: cfg, workspace: ws, sessionMCP: sessionMCP}
	b.proc = nil
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
	if len(names) == 0 {
		return
	}
	slog.Info("acp-remote: session mcp servers", "count", len(names), "servers", names)
	rttelemetry.RecordEvent(ctx, "acp.session_mcp", map[string]string{
		"count":   fmt.Sprintf("%d", len(names)),
		"servers": strings.Join(names, ","),
	})
}

func (b *bridge) setTurn(state turnState) {
	b.turnMu.Lock()
	b.turn = &state
	b.turnMu.Unlock()
}

func (b *bridge) clearTurn() {
	b.turnMu.Lock()
	b.turn = nil
	b.turnMu.Unlock()
}

func (b *bridge) currentTurn() *turnState {
	b.turnMu.Lock()
	defer b.turnMu.Unlock()
	return b.turn
}

func (b *bridge) sessionStoreDir(ctx context.Context) (string, error) {
	return b.workspace.Resolve(ctx, defaultSessionDir)
}

func (proc *subprocess) trackSession(sessionID agentkit.SessionID, acpSessionID acp.SessionId, state *sessionState) {
	proc.sessions.Store(sessionID, acpSessionID)
	proc.acpSessions.Store(acpSessionID, sessionID)
	proc.states.Store(sessionID, state)
}

func (b *bridge) ensureConn(ctx context.Context) (*subprocess, error) {
	b.mu.Lock()
	for b.proc != nil {
		if b.proc.alive() && (b.proc.authenticated || b.cfg.AuthMethod == "") {
			proc := b.proc
			b.mu.Unlock()
			return proc, nil
		}
		old, cancel := b.detachSubprocessLocked()
		b.mu.Unlock()
		terminateAndWaitSubprocess(old, cancel)
		b.mu.Lock()
	}

	cwd, err := b.resolveCwd(ctx)
	if err != nil {
		b.mu.Unlock()
		return nil, err
	}

	cmd := exec.CommandContext(ctx, b.cfg.Command[0], b.cfg.Command[1:]...)
	cmd.Dir = cwd
	configureCmdProcessGroup(cmd)
	cmd.Env = commandEnv(b.cfg.Env)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("acp stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("acp stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("acp start %v: %w", b.cfg.Command, err)
	}

	client := &bridgeClient{bridge: b}
	conn := acp.NewClientSideConnection(client, stdin, stdout)
	conn.SetLogger(slog.Default())

	initResp, err := conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
		},
		ClientInfo: &acp.Implementation{
			Name:    b.cfg.ClientName,
			Version: b.cfg.ClientVersion,
		},
	})
	if err != nil {
		terminateProcessGroup(cmd)
		_ = cmd.Wait()
		b.mu.Unlock()
		return nil, fmt.Errorf("acp initialize: %w", err)
	}
	slog.Info("acp-remote: connected", "protocol", initResp.ProtocolVersion)

	done := make(chan struct{})
	b.proc = &subprocess{cmd: cmd, conn: conn, client: client, done: done}
	go b.waitSubprocess(b.proc)
	if err := b.authenticateLocked(ctx, b.proc); err != nil {
		old, cancel := b.detachSubprocessLocked()
		b.mu.Unlock()
		terminateAndWaitSubprocess(old, cancel)
		return nil, err
	}
	proc := b.proc
	b.mu.Unlock()
	return proc, nil
}

func (b *bridge) authenticateLocked(ctx context.Context, proc *subprocess) error {
	if b.cfg.AuthMethod == "" {
		proc.authenticated = true
		return nil
	}
	if _, err := proc.conn.Authenticate(ctx, acp.AuthenticateRequest{
		MethodId: b.cfg.AuthMethod,
	}); err != nil {
		return fmt.Errorf("acp authenticate: %w (ensure cursor cli is logged in via agent login)", err)
	}
	proc.authenticated = true
	return nil
}

func (b *bridge) detachSubprocessLocked() (*subprocess, context.CancelFunc) {
	if b.proc == nil {
		return nil, nil
	}
	proc := b.proc
	b.proc = nil
	var cancel context.CancelFunc
	if b.promptCancel != nil {
		cancel = b.promptCancel
		b.promptCancel = nil
	}
	return proc, cancel
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

func (b *bridge) releaseSubprocess() {
	b.mu.Lock()
	proc, cancel := b.detachSubprocessLocked()
	b.mu.Unlock()
	terminateAndWaitSubprocess(proc, cancel)
}

func (b *bridge) waitSubprocess(proc *subprocess) {
	err := proc.cmd.Wait()
	if proc.done != nil {
		close(proc.done)
	}
	b.mu.Lock()
	if b.proc == proc {
		b.proc = nil
		cancel := b.promptCancel
		b.promptCancel = nil
		b.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if err != nil {
			slog.Warn("acp-remote: subprocess exited", "err", err)
		}
		return
	}
	b.mu.Unlock()
}

func (b *bridge) beginPromptContext(ctx context.Context) (context.Context, func()) {
	b.mu.Lock()
	if b.promptCancel != nil {
		b.promptCancel()
	}
	b.promptGen++
	gen := b.promptGen
	pctx, cancel := context.WithCancel(ctx)
	b.promptCancel = cancel
	b.mu.Unlock()
	return pctx, func() {
		b.mu.Lock()
		if b.promptGen == gen {
			b.promptCancel = nil
		}
		b.mu.Unlock()
		cancel()
	}
}

func (b *bridge) resolveCwd(ctx context.Context) (string, error) {
	if b.cfg.Cwd != "" {
		return b.workspace.Resolve(ctx, b.cfg.Cwd)
	}
	return b.workspace.Resolve(ctx, session.TenantToolWorkDir)
}

func (b *bridge) ensureACPSession(ctx context.Context, sessionID agentkit.SessionID, agentID agentkit.AgentID, sessionStore agentkit.SessionStore) (acp.SessionId, error) {
	proc, err := b.ensureConn(ctx)
	if err != nil {
		return "", err
	}
	if v, ok := proc.sessions.Load(sessionID); ok {
		return v.(acp.SessionId), nil
	}
	cwd, err := b.resolveCwd(ctx)
	if err != nil {
		return "", err
	}

	storeDir, err := b.sessionStoreDir(ctx)
	if err != nil {
		return "", err
	}
	bindPath, err := acpSessionBindPath(storeDir, sessionID, agentID)
	if err != nil {
		return "", err
	}

	mcpServers, err := b.resolveSessionMCP(ctx)
	if err != nil {
		return "", err
	}
	b.recordSessionMCP(ctx, mcpServers)

	if bind, ok, err := loadACPSessionBind(bindPath); err != nil {
		return "", err
	} else if ok && bind.Cwd == cwd {
		resp, err := proc.conn.ResumeSession(ctx, acp.ResumeSessionRequest{
			SessionId:  bind.ACPSessionID,
			Cwd:        cwd,
			McpServers: mcpServers,
		})
		if err == nil {
			state := &sessionState{}
			state.applyBootstrap(resp.ConfigOptions, resp.Modes)
			proc.trackSession(sessionID, bind.ACPSessionID, state)
			slog.Info("acp-remote: resumed acp session", "session_id", sessionID, "acp_session_id", bind.ACPSessionID)
			return bind.ACPSessionID, nil
		}
		slog.Warn("acp-remote: resume failed, creating new session", "session_id", sessionID, "err", err)
	}

	resp, err := proc.conn.NewSession(ctx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: mcpServers,
	})
	if err != nil {
		return "", fmt.Errorf("acp session/new: %w", err)
	}
	state := &sessionState{}
	state.applyBootstrap(resp.ConfigOptions, resp.Modes)
	proc.trackSession(sessionID, resp.SessionId, state)
	if err := saveACPSessionBind(bindPath, acpSessionBind{
		AgentID:      agentID,
		ACPSessionID: resp.SessionId,
		Cwd:          cwd,
	}); err != nil {
		slog.Debug("acp-remote: save session bind failed", "err", err)
	}
	if sessionStore != nil {
		sess, err := sessionStore.Get(ctx, sessionID)
		if err != nil {
			return "", err
		}
		if err := b.replayHistory(ctx, sess, resp.SessionId); err != nil {
			slog.Warn("acp-remote: history replay failed", "session_id", sessionID, "err", err)
		}
	}
	return resp.SessionId, nil
}

func (b *bridge) sessionState(sessionID agentkit.SessionID) (*sessionState, bool) {
	b.mu.Lock()
	proc := b.proc
	b.mu.Unlock()
	if proc == nil {
		return nil, false
	}
	v, ok := proc.states.Load(sessionID)
	if !ok {
		return nil, false
	}
	return v.(*sessionState), true
}

func (b *bridge) setConfigOption(ctx context.Context, acpSessionID acp.SessionId, opt configOptionRef, value string) (acp.SetSessionConfigOptionResponse, error) {
	proc, err := b.ensureConn(ctx)
	if err != nil {
		return acp.SetSessionConfigOptionResponse{}, err
	}
	if opt.boolean {
		parsed, err := parseBoolValue(value)
		if err != nil {
			return acp.SetSessionConfigOptionResponse{}, err
		}
		return proc.conn.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{
			Boolean: &acp.SetSessionConfigOptionBoolean{
				SessionId: acpSessionID,
				ConfigId:  opt.id,
				Type:      "boolean",
				Value:     parsed,
			},
		})
	}
	if len(opt.options) > 0 {
		if _, ok := opt.options[value]; !ok {
			return acp.SetSessionConfigOptionResponse{}, fmt.Errorf("invalid value %q for config %q", value, opt.id)
		}
	}
	return proc.conn.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: acpSessionID,
			ConfigId:  opt.id,
			Value:     acp.SessionConfigValueId(value),
		},
	})
}

func parseBoolValue(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "on", "yes":
		return true, nil
	case "false", "0", "off", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value %q (use true or false)", value)
	}
}

func (b *bridge) prompt(ctx context.Context, acpSessionID acp.SessionId, prompt []acp.ContentBlock) (acpPromptResponse, error) {
	proc, err := b.ensureConn(ctx)
	if err != nil {
		return acpPromptResponse{}, err
	}
	resp, err := proc.conn.Prompt(ctx, acp.PromptRequest{
		SessionId: acpSessionID,
		Prompt:    prompt,
	})
	if err != nil {
		return acpPromptResponse{}, err
	}
	return acpPromptResponse{stopReason: string(resp.StopReason)}, nil
}

func (b *bridge) cancel(ctx context.Context, acpSessionID acp.SessionId) error {
	b.mu.Lock()
	proc := b.proc
	b.mu.Unlock()
	if proc == nil {
		return nil
	}
	return proc.conn.Cancel(ctx, acp.CancelNotification{SessionId: acpSessionID})
}

// Close terminates the subprocess. Safe to call when not started.
func (b *bridge) Close() error {
	b.mu.Lock()
	proc, cancel := b.detachSubprocessLocked()
	b.mu.Unlock()
	terminateAndWaitSubprocess(proc, cancel)
	return nil
}

// bridgeClient implements acp.Client, delegating fs/permission to AgentKit capabilities.
type bridgeClient struct {
	bridge *bridge
}

var _ acp.Client = (*bridgeClient)(nil)

func (c *bridgeClient) SessionUpdate(ctx context.Context, params acp.SessionNotification) error {
	c.bridge.mu.Lock()
	proc := c.bridge.proc
	c.bridge.mu.Unlock()
	if proc != nil {
		if v, ok := proc.acpSessions.Load(params.SessionId); ok {
			if st, ok := proc.states.Load(v.(agentkit.SessionID)); ok {
				st.(*sessionState).applyUpdate(params.Update)
			}
		}
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
