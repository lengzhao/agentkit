package skill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	capsskill "github.com/lengzhao/agentkit/cap/skill"
)

const defaultScriptTimeout = 60 * time.Second

// RunResult is the outcome of executing one script inside a skill directory.
type RunResult struct {
	Name     string
	Path     string
	Script   string
	Args     []string
	ExitCode int
	Stdout   string
	Stderr   string
}

// RunScript executes a script relative to the skill directory identified by name.
func RunScript(ctx context.Context, reg capsskill.Registry, name, rel string, args []string, timeout time.Duration) (RunResult, error) {
	meta, err := reg.Load(ctx, name)
	if err != nil {
		return RunResult{}, err
	}
	if strings.TrimSpace(meta.Path) == "" {
		return RunResult{}, fmt.Errorf("skill %q has no on-disk directory", name)
	}
	clean, err := SanitizeRelativePath(rel)
	if err != nil {
		return RunResult{}, err
	}
	scriptPath := filepath.Join(meta.Path, clean)
	info, err := os.Stat(scriptPath)
	if err != nil {
		return RunResult{}, fmt.Errorf("skill script %q: %w", rel, err)
	}
	if info.IsDir() {
		return RunResult{}, fmt.Errorf("skill script %q is a directory", rel)
	}
	if timeout <= 0 {
		timeout = defaultScriptTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	prog, progArgs := resolveInterpreter(scriptPath)
	cmdArgs := append(progArgs, args...)
	cmd := exec.CommandContext(runCtx, prog, cmdArgs...)
	cmd.Dir = meta.Path
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if runCtx.Err() != nil {
			return RunResult{}, fmt.Errorf("skill script timeout after %s", timeout)
		} else {
			return RunResult{}, runErr
		}
	}
	return RunResult{
		Name:     meta.Name,
		Path:     meta.Path,
		Script:   clean,
		Args:     args,
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

func resolveInterpreter(scriptPath string) (string, []string) {
	switch strings.ToLower(filepath.Ext(scriptPath)) {
	case ".sh", ".bash":
		return "bash", []string{scriptPath}
	case ".py":
		return "python3", []string{scriptPath}
	default:
		if info, err := os.Stat(scriptPath); err == nil && info.Mode()&0o111 != 0 {
			return scriptPath, nil
		}
		return "bash", []string{scriptPath}
	}
}
