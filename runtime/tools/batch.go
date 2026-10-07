package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// PreflightTool runs visibility, policy, approval, and BeforeTool for one call.
// When runBody is false, result is final (deny, not found, etc.).
func (r *Runtime) PreflightTool(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, agentkit.ToolCall, bool, error) {
	sessionID := rctx.SessionIDFromContext(ctx)
	agentID := rctx.AgentIDFromContext(ctx)
	return r.preflightTool(ctx, call, sessionID, agentID)
}

// RunToolBody executes the tool body and AfterTool. Call only after a successful PreflightTool.
func (r *Runtime) RunToolBody(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	sessionID := rctx.SessionIDFromContext(ctx)
	agentID := rctx.AgentIDFromContext(ctx)
	return r.runToolBody(ctx, call, sessionID, agentID)
}

func (r *Runtime) preflightTool(ctx context.Context, call agentkit.ToolCall, sessionID agentkit.SessionID, agentID agentkit.AgentID) (agentkit.ToolResult, agentkit.ToolCall, bool, error) {
	if r.filter.active() && !r.filter.allows(call.Name) {
		return filteredOutResult(call), call, false, nil
	}

	if _, ok := r.lookupTool(call.Name); !ok {
		if err := r.refreshDynamic(ctx); err != nil {
			return agentkit.ToolResult{}, call, false, fmt.Errorf("refresh dynamic tools: %w", err)
		}
		if _, ok = r.lookupTool(call.Name); !ok {
			return deniedResult(call, "tool not found", "", nil), call, false, nil
		}
	}

	decision, err := r.evaluatePolicies(ctx, call)
	if err != nil {
		return agentkit.ToolResult{}, call, false, agentkit.AbortTurn(err)
	}
	switch decision.Kind {
	case agentkit.DecisionDeny:
		return deniedResult(call, decision.Reason, "", decision.Audit), call, false, nil
	case agentkit.DecisionAsk:
		allowed, reason, guidance, err := r.resolveAskDecision(ctx, &call, decision.Reason)
		if err != nil {
			return agentkit.ToolResult{}, call, false, agentkit.AbortTurn(err)
		}
		if !allowed {
			if reason == "" {
				reason = "approval denied"
			}
			return deniedResult(call, reason, guidance, nil), call, false, nil
		}
	}

	if r.hooks != nil {
		if err := r.hooks.BeforeTool(ctx, &call); err != nil {
			return agentkit.ToolResult{}, call, false, agentkit.AbortTurn(err)
		}
	}
	return agentkit.ToolResult{}, call, true, nil
}

func (r *Runtime) runToolBody(ctx context.Context, call agentkit.ToolCall, sessionID agentkit.SessionID, agentID agentkit.AgentID) (agentkit.ToolResult, error) {
	tool, ok := r.lookupTool(call.Name)
	if !ok {
		if err := r.refreshDynamic(ctx); err != nil {
			return agentkit.ToolResult{}, fmt.Errorf("refresh dynamic tools: %w", err)
		}
		tool, ok = r.lookupTool(call.Name)
		if !ok {
			return deniedResult(call, "tool not found", "", nil), nil
		}
	}

	execCtx := ctx
	cancel := func() {}
	if timeout := r.timeoutFor(call.Name); timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	slog.Info("tool execute", "tool", call.Name, "session_id", sessionID, "agent_id", agentID)
	started := time.Now()
	output, err := tool.Call(execCtx, call.Input)
	elapsed := time.Since(started)
	if err != nil {
		slog.Error("tool failed",
			"tool", call.Name,
			"session_id", sessionID,
			"agent_id", agentID,
			"duration", elapsed,
			"err", err,
		)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			return timeoutResult(call), nil
		}
		return agentkit.ToolResult{}, err
	}
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		slog.Warn("tool timed out",
			"tool", call.Name,
			"session_id", sessionID,
			"agent_id", agentID,
			"duration", elapsed,
		)
		return timeoutResult(call), nil
	}
	result := agentkit.ResultFromCall(call, output)
	slog.Info("tool done",
		"tool", call.Name,
		"session_id", sessionID,
		"agent_id", agentID,
		"duration", elapsed,
		"output_bytes", len(result.Content),
	)

	if r.hooks != nil {
		if err := r.hooks.AfterTool(ctx, &result); err != nil {
			return agentkit.ToolResult{}, agentkit.AbortTurn(err)
		}
	}
	return result, nil
}
