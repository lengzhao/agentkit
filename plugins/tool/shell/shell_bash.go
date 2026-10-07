package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/subprocess"
	"github.com/lengzhao/agentkit/runtime/tooloutput"
)

type ShellBashConfig struct {
	// WorkDir is working directory relative to the workspace root.
	WorkDir string `json:"workDir"`
	// TimeoutSeconds is per-command limit when the model omits timeout; 0 means no limit.
	TimeoutSeconds int `json:"timeoutSeconds"`
	// Commands optionally overrides env var names injected for shell-bash.<token>.
	// Default: scope is derived from the command's first token; keys come from L1 scopedEnv,
	// secrets.enc.json (/env add), or shell-bash.json manifest allowlist.
	Commands map[string][]string `json:"commands,omitempty"`
	// TrimEnv trims the child env to a minimal base (PATH/HOME/... + PWD) plus
	// scoped/extra pairs. Default false: the child inherits the full host env
	// (os.Environ) plus scoped pairs — secrets belong in scopedEnv / /env add.
	TrimEnv *bool `json:"trimEnv,omitempty"`
	// ExtraEnv adds static, non-secret KEY=value entries to the child env.
	// Secrets belong in credentials scopedEnv / /env add, not here.
	ExtraEnv map[string]string `json:"extraEnv,omitempty"`
}

type ShellBashDeps struct {
	Workspace   workspace.Service `json:"workspace"`
	Credentials credentials.Store `json:"credentials,omitempty"`
}

// SetDefaults implements pluginkit.Defaulter.
func (c *ShellBashConfig) SetDefaults() {
	if c.WorkDir == "" {
		c.WorkDir = "."
	}
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 60
	}
}

// Validate implements pluginkit.Validator.
func (c *ShellBashConfig) Validate() error {
	if c.TimeoutSeconds < 0 {
		return fmt.Errorf("tool/shell-bash timeoutSeconds must not be negative")
	}
	return nil
}

// ShellInput matches pi bash tool parameters.
type ShellInput struct {
	Command string   `json:"command" jsonschema:"Shell command to execute"`
	Timeout *float64 `json:"timeout,omitempty" jsonschema:"Timeout in seconds (optional, no default timeout)"`
}

type ShellOutput struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type bashExecutor struct {
	relWorkDir  string
	workspace   workspace.Service
	credentials credentials.Store
	commands    map[string][]string
	cfg         ShellBashConfig
}

// NewShellBash registers tool/shell-bash: Execute bash commands rooted in the workspace (tool name: bash).
//
// Best practices:
//   - Keep commands non-interactive; avoid pagers and prompts.
//   - For unattended runs pair with policy/shell-allowlist in strict mode.
func NewShellBash(cfg ShellBashConfig, deps ShellBashDeps) (agentkit.Tool, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("tool/shell-bash requires workspace")
	}
	if deps.Credentials != nil {
		if _, ok := deps.Credentials.(credentials.EnvPairResolver); !ok {
			slog.Warn("tool/shell-bash: deps.credentials is not credentials/integrations; scoped env injection disabled")
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
	exec := &bashExecutor{
		relWorkDir:  cfg.WorkDir,
		workspace:   deps.Workspace,
		credentials: deps.Credentials,
		commands:    commands,
		cfg:         cfg,
	}

	tool, err := agentkit.NewTool[ShellInput, string]("bash", func(ctx context.Context, input ShellInput) (string, error) {
		return exec.runForModel(ctx, input)
	}).Description(tooloutput.BashToolDescription()).
		Sequential().Build()
	if err != nil {
		return nil, err
	}
	return &shellBashBundle{tool: tool, exec: exec}, nil
}

func (e *bashExecutor) runForModel(ctx context.Context, input ShellInput) (string, error) {
	out, runErr := e.run(ctx, input.Command, input.Timeout)
	workDir, _ := e.workspace.Resolve(ctx, e.relWorkDir)
	return tooloutput.BashModelResult(ctx, e.workspace, workDir, tooloutput.BashStreams{
		Stdout: out.Stdout, Stderr: out.Stderr, ExitCode: out.ExitCode,
	}, runErr)
}

func (e *bashExecutor) run(ctx context.Context, command string, perCallTimeout *float64) (ShellOutput, error) {
	limit, err := tooloutput.ResolveRunTimeout(perCallTimeout, e.cfg.TimeoutSeconds)
	if err != nil {
		return ShellOutput{}, err
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if limit > 0 {
		runCtx, cancel = context.WithTimeout(ctx, limit)
		defer cancel()
	}

	workDir, err := e.workspace.Resolve(ctx, e.relWorkDir)
	if err != nil {
		return ShellOutput{}, err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return ShellOutput{}, fmt.Errorf("mkdir work dir: %w", err)
	}

	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
	subprocess.PrepareExecCmd(cmd)
	cmd.Dir = workDir
	cmd.Env = subprocessEnv(ctx, workDir, command, e.commands, e.credentials, e.cfg)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	out := ShellOutput{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
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
		return ShellOutput{}, err
	}
	return out, nil
}
