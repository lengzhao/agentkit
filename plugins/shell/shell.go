package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/lengzhao/agentkit/cap/shell"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	// WorkDir is working directory relative to the workspace root.
	WorkDir string `json:"workDir"`
	// TimeoutSeconds is per-command limit; 0 falls back to the built-in default.
	TimeoutSeconds int `json:"timeoutSeconds"`
}

type Deps struct {
	Workspace workspace.Service `json:"workspace"`
}

type Executor struct {
	relWorkDir string
	timeout    time.Duration
	workspace  workspace.Service
}

func init() {
	pluginkit.Register("shell/bash", New)
}

// New registers shell/bash: Run bash commands with a per-command timeout, rooted in the workspace.
func New(cfg Config, deps Deps) (shell.Executor, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("shell/bash requires workspace")
	}
	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "."
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Executor{relWorkDir: workDir, timeout: timeout, workspace: deps.Workspace}, nil
}

func (e *Executor) Run(ctx context.Context, req shell.Request) (shell.Result, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = e.timeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workDir, err := e.workspace.Resolve(ctx, e.relWorkDir)
	if err != nil {
		return shell.Result{}, err
	}

	cmd := exec.CommandContext(runCtx, "bash", "-lc", req.Command)
	cmd.Dir = workDir
	if req.WorkDir != "" {
		cmd.Dir = req.WorkDir
	}
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if runCtx.Err() != nil {
			return shell.Result{}, fmt.Errorf("shell timeout after %s", timeout)
		} else {
			return shell.Result{}, err
		}
	}
	return shell.Result{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}
