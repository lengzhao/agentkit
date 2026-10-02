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
	Command string `json:"command" jsonschema:"Shell command to execute"`
}

// ShellOutput is the bash tool output (same schema as tool/shell-bash).
type ShellOutput struct {
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

func init() {
	pluginkit.Register("tool/shell-bwrap", New)
}

type executor struct {
	relWorkDir  string
	timeout     time.Duration
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
		timeout:     time.Duration(cfg.TimeoutSeconds) * time.Second,
		workspace:   d.Workspace,
		credentials: d.Credentials,
		sandbox:     d.Sandbox,
		commands:    commands,
		maxOutput:   cfg.MaxOutputBytes,
	}
	tool, err := agentkit.NewTool[ShellInput, ShellOutput]("bash", func(ctx context.Context, input ShellInput) (ShellOutput, error) {
		return ex.run(ctx, input.Command)
	}).Description("Execute a bash command in the workspace (default cwd is the absolute agent work directory). Prefer absolute paths in commands and when cd-ing to skill or global directories.").Build()
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
	out, err := c.exec.run(ctx, command)
	if err != nil {
		return "", err
	}
	return formatShellOutput(out), nil
}

func formatShellOutput(out ShellOutput) string {
	var b strings.Builder
	if out.Stdout != "" {
		b.WriteString(out.Stdout)
	}
	if out.Stderr != "" {
		if b.Len() > 0 && !strings.HasSuffix(out.Stdout, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(out.Stderr)
	}
	if out.ExitCode != 0 {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "[exit %d]", out.ExitCode)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (e *executor) run(ctx context.Context, command string) (ShellOutput, error) {
	runCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	workDir, err := e.workspace.Resolve(ctx, e.relWorkDir)
	if err != nil {
		return ShellOutput{}, err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return ShellOutput{}, fmt.Errorf("mkdir work dir: %w", err)
	}
	if real, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = real
	}

	argv := []string{"bash", "-lc", command}
	argv, err = e.sandbox.WrapArgv(ctx, workDir, argv)
	if err != nil {
		return ShellOutput{}, err
	}
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = workDir
	cmd.Env = shellSubprocessEnv(ctx, workDir, command, e.commands, e.credentials, e.sandbox.Enabled())
	// Output truncation: prevents a command with unbounded output (e.g. yes)
	// from blowing up runner memory.
	stdout := &truncBuffer{limit: e.maxOutput}
	stderr := &truncBuffer{limit: e.maxOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if runCtx.Err() != nil {
			return ShellOutput{}, fmt.Errorf("shell timeout after %s", e.timeout)
		} else {
			return ShellOutput{}, err
		}
	}
	stderrText := stderr.String()
	out := ShellOutput{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderrText,
	}
	if stdout.truncated {
		out.Stdout += "\n...[stdout truncated]"
	}
	if stderr.truncated {
		out.Stderr += "\n...[stderr truncated]"
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
