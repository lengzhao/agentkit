// Package shellexec provides cap/shell.Executor as a standalone plugin, so
// schedules (and any other consumer) can run script jobs without the host
// writing its own executor.
//
// Consumers of cap/shell.Executor - plugins/schedule/cron script jobs and the
// headless worker - previously had no implementation to point at: tool/shell-bash
// is an agentkit.Tool (a model-facing tool), not an Executor. Hosts had to supply
// their own, so script jobs were dead configuration by default.
package shellexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	capshell "github.com/lengzhao/agentkit/cap/shell"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/subprocess"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("tool/shell-exec", New)
}

// Config configures the executor.
type Config struct {
	// WorkDir is the default working directory, relative to the workspace.
	// A request-level WorkDir overrides it.
	WorkDir string `json:"workDir,omitempty"`
	// TimeoutSeconds bounds a run that does not set its own deadline. 0 means
	// no default limit (the caller's context still applies).
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// TrimEnv restricts the child to a minimal base (PATH/HOME/TMPDIR/PWD)
	// instead of inheriting the daemon environment. Prefer this for scheduled
	// jobs, which should not depend on whatever the supervisor exported.
	TrimEnv bool `json:"trimEnv,omitempty"`
	// ExtraEnv adds static KEY=value pairs to the child environment. Secrets
	// belong in the host's credential store, not here.
	ExtraEnv map[string]string `json:"extraEnv,omitempty"`
}

// SetDefaults implements pluginkit.Defaulter.
func (c *Config) SetDefaults() {
	if c.WorkDir == "" {
		c.WorkDir = "work"
	}
}

// Validate implements pluginkit.Validator.
func (c *Config) Validate() error {
	if c.TimeoutSeconds < 0 {
		return fmt.Errorf("tool/shell-exec timeoutSeconds must not be negative")
	}
	return nil
}

// Deps wires the workspace used to resolve the working directory.
type Deps struct {
	Workspace workspace.Service `json:"workspace"`
}

// Executor implements cap/shell.Executor with bash.
type Executor struct {
	cfg Config
	ws  workspace.Service
}

// New registers tool/shell-exec: bash-backed cap/shell.Executor.
func New(cfg Config, deps Deps) (capshell.Executor, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("tool/shell-exec requires workspace")
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Executor{cfg: cfg, ws: deps.Workspace}, nil
}

// Run executes req.Command with bash -lc. A non-zero exit is reported through
// Result.ExitCode (not an error): the caller decides whether that is fatal.
func (e *Executor) Run(ctx context.Context, req capshell.Request) (capshell.Result, error) {
	command := req.Command
	if command == "" {
		return capshell.Result{}, errors.New("tool/shell-exec: empty command")
	}
	limit := req.Timeout
	if limit <= 0 && e.cfg.TimeoutSeconds > 0 {
		limit = time.Duration(e.cfg.TimeoutSeconds) * time.Second
	}
	runCtx := ctx
	if limit > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, limit)
		defer cancel()
	}

	relDir := req.WorkDir
	if relDir == "" {
		relDir = e.cfg.WorkDir
	}
	workDir, err := e.ws.Resolve(ctx, relDir)
	if err != nil {
		return capshell.Result{}, fmt.Errorf("resolve work dir: %w", err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return capshell.Result{}, fmt.Errorf("mkdir work dir: %w", err)
	}

	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
	subprocess.PrepareExecCmd(cmd)
	cmd.Dir = workDir
	cmd.Env = e.env(workDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	out := capshell.Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if runErr != nil {
		if ctxErr := runCtx.Err(); ctxErr != nil {
			if errors.Is(ctxErr, context.DeadlineExceeded) {
				return out, fmt.Errorf("tool/shell-exec timed out after %s", limit)
			}
			return out, fmt.Errorf("tool/shell-exec cancelled: %w", ctxErr)
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			out.ExitCode = exitErr.ExitCode()
			slog.Debug("tool/shell-exec: non-zero exit", "exit_code", out.ExitCode)
			return out, nil
		}
		return out, runErr
	}
	return out, nil
}

// env builds the child environment. Without TrimEnv the child inherits the
// daemon environment, which is what an interactive host expects; a scheduled
// job should set TrimEnv so it does not inherit an operator's shell.
func (e *Executor) env(workDir string) []string {
	extra := map[string]string{"PWD": workDir}
	for k, v := range e.cfg.ExtraEnv {
		extra[k] = v
	}
	if !e.cfg.TrimEnv {
		out := os.Environ()
		for k, v := range extra {
			out = append(out, k+"="+v)
		}
		return out
	}
	out := make([]string, 0, len(extra)+4)
	for _, key := range []string{"HOME", "TMPDIR", "LANG", "LC_ALL"} {
		if v, ok := os.LookupEnv(key); ok {
			out = append(out, key+"="+v)
		}
	}
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/local/bin:/usr/bin:/bin"
	}
	out = append(out, "PATH="+path)
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}
