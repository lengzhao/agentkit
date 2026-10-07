// Package shellbwrap provides tool/shell-bwrap: a tool/shell-bash compatible
// shell (the bash tool + the /shell command) whose sandbox view and bwrap
// wrapping are delegated to a sandbox/bwrap instance (runtime/sandbox) — the
// view config has a single source shared by shell / ACP subprocesses and
// in-process fs enforcement.
package shellbwrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	capsandbox "github.com/lengzhao/agentkit/cap/sandbox"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/subprocess"
	"github.com/lengzhao/agentkit/runtime/tooloutput"
	"github.com/lengzhao/pluginkit"
)

// Config configures tool/shell-bwrap (the sandbox view config lives on the
// sandbox/bwrap instance).
type Config struct {
	WorkDir        string              `json:"workDir"`
	TimeoutSeconds int                 `json:"timeoutSeconds"`
	Commands       map[string][]string `json:"commands"`
	// MaxOutputBytes caps the collected stdout/stderr bytes each; excess is
	// truncated and marked. <=0 uses the built-in default (1 MiB). Prevents a
	// command with unbounded output from blowing up runner memory.
	MaxOutputBytes int `json:"maxOutputBytes"`
}

// SetDefaults implements pluginkit.Defaulter.
func (c *Config) SetDefaults() {
	if c.WorkDir == "" {
		c.WorkDir = "."
	}
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 60
	}
	if c.MaxOutputBytes == 0 {
		c.MaxOutputBytes = 1 << 20 // 1 MiB
	}
}

// Validate implements pluginkit.Validator.
func (c *Config) Validate() error {
	if c.TimeoutSeconds < 0 {
		return fmt.Errorf("tool/shell-bwrap timeoutSeconds must not be negative")
	}
	if c.MaxOutputBytes < 0 {
		return fmt.Errorf("tool/shell-bwrap maxOutputBytes must not be negative")
	}
	return nil
}

// Deps for tool/shell-bwrap.
type Deps struct {
	Workspace   workspace.Service  `json:"workspace"`
	Credentials credentials.Store  `json:"credentials,omitempty"`
	Sandbox     capsandbox.Service `json:"sandbox"`
}

// ShellInput is the bash tool input (same schema as tool/shell-bash).
type ShellInput struct {
	Command string   `json:"command" jsonschema:"Shell command to execute"`
	Timeout *float64 `json:"timeout,omitempty" jsonschema:"Timeout in seconds (optional, no default timeout)"`
}

type shellOutput struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

func init() {
	pluginkit.Register("tool/shell-bwrap", New)
}

type executor struct {
	relWorkDir  string
	cfg         Config
	workspace   workspace.Service
	credentials credentials.Store
	sandbox     capsandbox.Service
	commands    map[string][]string
	maxOutput   int
}

type bundle struct {
	tool agentkit.Tool
	exec *executor
}

// New constructs tool/shell-bwrap. Sandbox behavior (mode / fail closed /
// view config) is owned by the deps.sandbox (sandbox/bwrap) instance.
func New(cfg Config, d Deps) (agentkit.Tool, error) {
	if d.Workspace == nil {
		return nil, fmt.Errorf("tool/shell-bwrap requires workspace")
	}
	if d.Sandbox == nil {
		return nil, fmt.Errorf("tool/shell-bwrap requires sandbox (sandbox/bwrap instance)")
	}
	if d.Credentials != nil {
		if _, ok := d.Credentials.(credentials.EnvPairResolver); !ok {
			slog.Warn("tool/shell-bwrap: deps.credentials is not credentials/integrations; scoped env injection disabled")
		}
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	commands := cfg.Commands
	if commands == nil {
		commands = map[string][]string{}
	}
	ex := &executor{
		relWorkDir:  cfg.WorkDir,
		cfg:         cfg,
		workspace:   d.Workspace,
		credentials: d.Credentials,
		sandbox:     d.Sandbox,
		commands:    commands,
		maxOutput:   cfg.MaxOutputBytes,
	}
	tool, err := agentkit.NewTool[ShellInput, string]("bash", func(ctx context.Context, input ShellInput) (string, error) {
		out, runErr := ex.run(ctx, input.Command, input.Timeout)
		workDir, _ := d.Workspace.Resolve(ctx, cfg.WorkDir)
		return tooloutput.BashModelResult(ctx, d.Workspace, workDir, tooloutput.BashStreams{
			Stdout: out.Stdout, Stderr: out.Stderr, ExitCode: out.ExitCode,
		}, runErr)
	}).Description(tooloutput.BashToolDescription()).
		Sequential().Build()
	if err != nil {
		return nil, err
	}
	return &bundle{tool: tool, exec: ex}, nil
}

func (b *bundle) Name() string { return b.tool.Name() }

func (b *bundle) Description() string { return b.tool.Description() }

func (b *bundle) InputSchema() agentkit.JSONSchema { return b.tool.InputSchema() }

func (b *bundle) Call(ctx context.Context, input json.RawMessage) (string, error) {
	return b.tool.Call(ctx, input)
}

func (b *bundle) Commands() []agentkit.Command {
	return []agentkit.Command{slashCommand{exec: b.exec}}
}

type slashCommand struct {
	exec *executor
}

func (slashCommand) Name() string  { return "shell" }
func (slashCommand) Alias() string { return "sh" }
func (slashCommand) Description() string {
	return "run a shell command locally without invoking the model"
}

func (slashCommand) SanitizeArgsForLog(args string) string {
	return agentkit.RedactSlashArgsForLog(args)
}

func (c slashCommand) CommandExec(ctx context.Context, args string) (string, error) {
	command := strings.TrimSpace(args)
	if command == "" {
		return "", fmt.Errorf("usage: /shell <command>")
	}
	out, err := c.exec.run(ctx, command, nil)
	if err != nil {
		if te, ok := tooloutput.AsTimeoutError(err); ok {
			return "", fmt.Errorf("shell timeout after %g seconds", te.Seconds)
		}
		return "", err
	}
	return tooloutput.SlashCommandText(out.Stdout, out.Stderr, out.ExitCode), nil
}

func (e *executor) run(ctx context.Context, command string, perCallTimeout *float64) (shellOutput, error) {
	limit, err := tooloutput.ResolveRunTimeout(perCallTimeout, e.cfg.TimeoutSeconds)
	if err != nil {
		return shellOutput{}, err
	}
	runCtx := ctx
	var cancel context.CancelFunc
	if limit > 0 {
		runCtx, cancel = context.WithTimeout(ctx, limit)
		defer cancel()
	}

	workDir, err := e.workspace.Resolve(ctx, e.relWorkDir)
	if err != nil {
		return shellOutput{}, err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return shellOutput{}, fmt.Errorf("mkdir work dir: %w", err)
	}
	if real, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = real
	}

	argv := []string{"bash", "-lc", command}
	argv, err = e.sandbox.WrapArgv(ctx, workDir, argv)
	if err != nil {
		return shellOutput{}, err
	}
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	subprocess.PrepareExecCmd(cmd)
	cmd.Dir = workDir
	cmd.Env = shellSubprocessEnv(ctx, workDir, command, e.commands, e.credentials, e.sandbox.Enabled())
	// Output truncation: prevents a command with unbounded output (e.g. yes)
	// from blowing up runner memory.
	stdout := &truncBuffer{limit: e.maxOutput}
	stderr := &truncBuffer{limit: e.maxOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	stderrText := stderr.String()
	out := shellOutput{
		Stdout: stdout.String(),
		Stderr: stderrText,
	}
	if stdout.truncated {
		out.Stdout += "\n...[stdout truncated]"
	}
	if stderr.truncated {
		out.Stderr += "\n...[stderr truncated]"
	}
	if err != nil {
		if runCtx.Err() != nil {
			if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
				out.ExitCode = 1
				secs := float64(limit) / float64(time.Second)
				if perCallTimeout != nil {
					secs = *perCallTimeout
				} else if e.cfg.TimeoutSeconds > 0 {
					secs = float64(e.cfg.TimeoutSeconds)
				}
				return out, &tooloutput.TimeoutError{Seconds: secs}
			}
			return out, fmt.Errorf("%w: %w", tooloutput.ErrShellCancelled, runCtx.Err())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			out.ExitCode = exitErr.ExitCode()
			return out, nil
		}
		return shellOutput{}, err
	}
	return out, nil
}

// truncBuffer collects bounded output: bytes beyond limit are dropped and the
// buffer is marked truncated.
type truncBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (w *truncBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if w.limit > 0 {
		remaining := w.limit - w.buf.Len()
		if remaining <= 0 {
			w.truncated = true
			return n, nil
		}
		if len(p) > remaining {
			p = p[:remaining]
			w.truncated = true
		}
	}
	w.buf.Write(p)
	return n, nil
}

func (w *truncBuffer) String() string { return w.buf.String() }

var _ agentkit.CommandProvider = (*bundle)(nil)
