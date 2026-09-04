package acpremote

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type turnState struct {
	ctx       context.Context
	emitter   *updateEmitter
	sessionID agentkit.SessionID
	agentID   agentkit.AgentID
}

type acpPromptResponse struct {
	stopReason string
}

type bridge struct {
	cfg       Config
	workspace workspace.Service

	mu    sync.Mutex
	proc  *subprocess
	turn  *turnState
	turnMu sync.Mutex
}

type subprocess struct {
	cmd           *exec.Cmd
	conn          *acp.ClientSideConnection
	client        *bridgeClient
	authenticated bool
	sessions      sync.Map // agentkit.SessionID -> acp.SessionId
}

func newBridge(cfg Config, ws workspace.Service) *bridge {
	b := &bridge{cfg: cfg, workspace: ws}
	b.proc = nil
	return b
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

func (b *bridge) ensureConn(ctx context.Context) (*subprocess, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.proc != nil {
		if b.proc.authenticated || b.cfg.AuthMethod == "" {
			return b.proc, nil
		}
		b.killProcLocked()
	}

	cmd := exec.CommandContext(ctx, b.cfg.Command[0], b.cfg.Command[1:]...)
	cmd.Env = commandEnv(b.cfg.Env)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("acp stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
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
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("acp initialize: %w", err)
	}
	slog.Info("acp-remote: connected", "protocol", initResp.ProtocolVersion)

	b.proc = &subprocess{cmd: cmd, conn: conn, client: client}
	if err := b.authenticateLocked(ctx, b.proc); err != nil {
		return nil, err
	}
	return b.proc, nil
}

func (b *bridge) authenticateLocked(ctx context.Context, proc *subprocess) error {
	if b.cfg.AuthMethod == "" {
		proc.authenticated = true
		return nil
	}
	if _, err := proc.conn.Authenticate(ctx, acp.AuthenticateRequest{
		MethodId: b.cfg.AuthMethod,
	}); err != nil {
		b.killProcLocked()
		return fmt.Errorf("acp authenticate: %w (ensure cursor cli is logged in via agent login)", err)
	}
	proc.authenticated = true
	return nil
}

func (b *bridge) killProcLocked() {
	if b.proc == nil {
		return
	}
	if b.proc.cmd.Process != nil {
		_ = b.proc.cmd.Process.Kill()
	}
	b.proc = nil
}

func (b *bridge) resolveCwd(ctx context.Context) (string, error) {
	if b.cfg.Cwd != "" {
		return b.workspace.Resolve(ctx, b.cfg.Cwd)
	}
	return b.workspace.Resolve(ctx, ".")
}

func (b *bridge) ensureACPSession(ctx context.Context, sessionID agentkit.SessionID) (acp.SessionId, error) {
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
	resp, err := proc.conn.NewSession(ctx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		return "", fmt.Errorf("acp session/new: %w", err)
	}
	proc.sessions.Store(sessionID, resp.SessionId)
	return resp.SessionId, nil
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
	defer b.mu.Unlock()
	b.killProcLocked()
	return nil
}

// bridgeClient implements acp.Client, delegating fs/permission to AgentKit capabilities.
type bridgeClient struct {
	bridge *bridge
}

var _ acp.Client = (*bridgeClient)(nil)

func (c *bridgeClient) SessionUpdate(ctx context.Context, params acp.SessionNotification) error {
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
