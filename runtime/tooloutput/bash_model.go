package tooloutput

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/lengzhao/agentkit/cap/workspace"
)

// BashStreams is raw subprocess output before pi-style formatting.
type BashStreams struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// ErrShellCancelled marks a shell command stopped by context cancellation.
var ErrShellCancelled = errors.New("shell cancelled")

// TimeoutError is returned when a shell command hits its time limit.
type TimeoutError struct {
	Seconds float64
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("timeout:%g", e.Seconds)
}

// AsTimeoutError reports whether err is a TimeoutError.
func AsTimeoutError(err error) (*TimeoutError, bool) {
	var te *TimeoutError
	if errors.As(err, &te) {
		return te, true
	}
	return nil, false
}

// BashModelResult formats combined output for the bash tool (pi coding-agent semantics).
func BashModelResult(ctx context.Context, ws workspace.Service, workDir string, out BashStreams, runErr error) (string, error) {
	combined := CombineStdoutStderr(out.Stdout, out.Stderr)
	trunc := TruncateTail(combined, 0, 0)
	var spillAbs string
	if trunc.Truncated && workDir != "" {
		abs, spillErr := WriteBashSpill(workDir, combined)
		if spillErr != nil {
			slog.WarnContext(ctx, "bash tool: spill write failed", "err", spillErr)
		} else {
			spillAbs = abs
		}
	}
	text := FormatBashModelText(ctx, ws, trunc, spillAbs)
	if runErr != nil {
		if te, ok := AsTimeoutError(runErr); ok {
			return "", fmt.Errorf("%s\n\nCommand timed out after %g seconds", text, te.Seconds)
		}
		if errors.Is(runErr, ErrShellCancelled) || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			return "", fmt.Errorf("%s\n\nCommand aborted", text)
		}
		return "", runErr
	}
	if out.ExitCode != 0 {
		return "", fmt.Errorf("%s\n\nCommand exited with code %d", text, out.ExitCode)
	}
	return text, nil
}
