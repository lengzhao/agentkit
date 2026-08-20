package shellbash

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/lengzhao/agentkit/cap/shell"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	WorkDir        string `json:"workDir"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

type Executor struct {
	workDir string
	timeout time.Duration
}

func init() {
	pluginkit.Register("shell/bash", New)
}

func New(cfg Config) (shell.Executor, error) {
	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "."
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Executor{workDir: abs, timeout: timeout}, nil
}

func (e *Executor) Run(ctx context.Context, req shell.Request) (shell.Result, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = e.timeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-lc", req.Command)
	cmd.Dir = e.workDir
	if req.WorkDir != "" {
		cmd.Dir = req.WorkDir
	}
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
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
