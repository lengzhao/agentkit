package agentkit

import (
	"context"
	"errors"
)

// TurnAbortError marks an error that must end the current agent turn (or step
// segment) without synthesizing a tool result. Use AbortTurn when returning from
// ToolRuntime.Execute for infrastructure failures (hooks, policy runtime, etc.).
// Context cancellation is treated as turn-aborting without wrapping.
type TurnAbortError struct {
	Err error
}

func (e TurnAbortError) Error() string {
	if e.Err == nil {
		return "turn aborted"
	}
	return e.Err.Error()
}

func (e TurnAbortError) Unwrap() error { return e.Err }

// AbortTurn wraps cause as a turn-fatal error for ToolRuntime.Execute.
func AbortTurn(cause error) error {
	if cause == nil {
		return nil
	}
	return TurnAbortError{Err: cause}
}

// ToolTimeoutResultText is the model-visible body for a per-tool timeout.
const ToolTimeoutResultText = "tool execution timed out"

// ToolResultFromTimeout builds the standard tool result for a timed-out tool call.
func ToolResultFromTimeout(call ToolCall) ToolResult {
	return ToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: ToolTimeoutResultText,
		Audit:   map[string]string{"decision": "timeout"},
	}
}

// IsTurnAbort reports whether err should end the turn instead of becoming a
// model-visible tool result. Per-tool timeouts are never turn-aborting; they
// are surfaced via ToolResultFromTimeout (tools/runtime normally returns them
// with err=nil before the agent loop).
func IsTurnAbort(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var abort TurnAbortError
	return errors.As(err, &abort)
}

// RecoverToolExecute maps ToolRuntime.Execute outcomes for the agent tool loop:
// recoverable errors become tool results; turn-abort errors propagate.
func RecoverToolExecute(call ToolCall, result ToolResult, err error) (ToolResult, error) {
	if err == nil {
		return result, nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ToolResultFromTimeout(call), nil
	}
	if IsTurnAbort(err) {
		return ToolResult{}, err
	}
	return ToolResultFromExecuteError(call, err), nil
}

// ToolResultFromExecuteError builds a tool result from a recoverable Execute error.
func ToolResultFromExecuteError(call ToolCall, err error) ToolResult {
	reason := "tool execution failed"
	if err != nil && err.Error() != "" {
		reason = err.Error()
	}
	return ToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: reason,
		Audit:   map[string]string{"decision": "error", "reason": reason},
	}
}
