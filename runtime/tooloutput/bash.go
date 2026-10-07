package tooloutput

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/lengzhao/agentkit/cap/workspace"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
)

const maxTimeoutSeconds = 2_147_483_647 / 1000

// BashToolDescription matches pi coding-agent / durable bash tool description.
func BashToolDescription() string {
	return fmt.Sprintf(
		"Execute a bash command in the current working directory. Returns combined stdout and stderr. Output is truncated to last %d lines or %dKB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds.",
		DefaultMaxLines,
		DefaultMaxBytes/1024,
	)
}

// CombineStdoutStderr merges process streams in order (stdout then stderr).
func CombineStdoutStderr(stdout, stderr string) string {
	var b strings.Builder
	if stdout != "" {
		b.WriteString(stdout)
	}
	if stderr != "" {
		if b.Len() > 0 && !strings.HasSuffix(stdout, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(stderr)
	}
	return b.String()
}

// WriteBashSpill persists full command output under workDir (pi-bash-* temp file).
func WriteBashSpill(workDir string, full string) (absPath string, err error) {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(workDir, "pi-bash-")
	if err != nil {
		return "", err
	}
	absPath = f.Name()
	if _, err := f.WriteString(full); err != nil {
		f.Close()
		os.Remove(absPath)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(absPath)
		return "", err
	}
	return absPath, nil
}

// FormatBashModelText builds model-visible bash output with pi-style truncation hints.
func FormatBashModelText(ctx context.Context, ws workspace.Service, trunc TailTruncation, spillAbs string) string {
	text := trunc.Content
	if !trunc.Truncated {
		if text == "" {
			return "(no output)"
		}
		return text
	}
	display := spillAbs
	if ws != nil && spillAbs != "" {
		display = rtmedia.AgentLLMPath(ctx, ws, spillAbs)
	}
	endLine := trunc.TotalLines
	startLine := endLine - trunc.OutputLines + 1
	if startLine < 1 {
		startLine = 1
	}
	switch {
	case trunc.LastLinePartial:
		text += fmt.Sprintf("\n\n[Showing last %s of line %d. Full output: %s]", formatSize(trunc.OutputBytes), endLine, display)
	case trunc.TruncatedBy == "lines":
		text += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Full output: %s]", startLine, endLine, trunc.TotalLines, display)
	default:
		text += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Full output: %s]", startLine, endLine, trunc.TotalLines, formatSize(trunc.MaxBytes), display)
	}
	return text
}

func formatSize(bytes int) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%dB", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	}
}

// SlashCommandText formats /shell output (combined streams + optional exit suffix).
func SlashCommandText(stdout, stderr string, exitCode int) string {
	text := CombineStdoutStderr(stdout, stderr)
	if exitCode != 0 {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += fmt.Sprintf("[exit %d]", exitCode)
	}
	return strings.TrimRight(text, "\n")
}

// ValidateTimeoutSeconds checks per-call timeout (pi bash tool).
func ValidateTimeoutSeconds(timeout *float64) error {
	if timeout == nil {
		return nil
	}
	if !isFinitePositive(*timeout) {
		return fmt.Errorf("Invalid timeout: must be a finite number of seconds")
	}
	if *timeout > maxTimeoutSeconds {
		return fmt.Errorf("Invalid timeout: maximum is %d seconds", maxTimeoutSeconds)
	}
	return nil
}

func isFinitePositive(v float64) bool {
	return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v)
}

// ResolveRunTimeout returns the limit for a command: per-call wins, else configSeconds when > 0.
func ResolveRunTimeout(perCall *float64, configSeconds int) (time.Duration, error) {
	if err := ValidateTimeoutSeconds(perCall); err != nil {
		return 0, err
	}
	if perCall != nil {
		return time.Duration(*perCall * float64(time.Second)), nil
	}
	if configSeconds > 0 {
		return time.Duration(configSeconds) * time.Second, nil
	}
	return 0, nil
}
