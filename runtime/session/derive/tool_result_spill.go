package derive

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
	cw "github.com/lengzhao/agentkit/cap/workspace"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// AuditSpillPath records the workspace-relative spill file for a truncated tool result.
const AuditSpillPath = "spill_path"

// PrepareToolResultForStorage keeps the model-visible body bounded while persisting
// the full output to workspace (pi-style spill). When workspace is unavailable,
// falls back to in-event truncation only.
func PrepareToolResultForStorage(ctx context.Context, sessionID agentkit.SessionID, result agentkit.ToolResult, maxViewBytes int) (agentkit.ToolResult, error) {
	if maxViewBytes <= 0 {
		maxViewBytes = DefaultMaxStoredTextBytes
	}
	if result.Audit != nil && result.Audit[AuditSpillPath] != "" {
		return result, nil
	}
	if len(result.Content) <= maxViewBytes {
		return result, nil
	}

	ws := rctx.WorkspaceServiceFromContext(ctx)
	if ws == nil {
		return TruncateToolResult(result, maxViewBytes), nil
	}

	rel := toolSpillRelPath(ws, sessionID, result.ID)
	abs, err := ws.Resolve(ctx, rel)
	if err != nil {
		slog.WarnContext(ctx, "tool result spill resolve failed; truncating in event", "path", rel, "error", err)
		return TruncateToolResult(result, maxViewBytes), nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		slog.WarnContext(ctx, "tool result spill mkdir failed; truncating in event", "path", abs, "error", err)
		return TruncateToolResult(result, maxViewBytes), nil
	}
	if err := os.WriteFile(abs, []byte(result.Content), 0o644); err != nil {
		slog.WarnContext(ctx, "tool result spill write failed; truncating in event", "path", abs, "error", err)
		return TruncateToolResult(result, maxViewBytes), nil
	}

	out := result
	out.Content = toolResultViewWithSpillHint(ctx, ws, result.Content, rel, maxViewBytes)
	if out.Audit == nil {
		out.Audit = make(map[string]string, 1)
	}
	out.Audit[AuditSpillPath] = rel
	return out, nil
}

func toolResultViewWithSpillHint(ctx context.Context, ws cw.Service, full string, spillRel string, maxViewBytes int) string {
	display := spillRel
	if ws != nil {
		display = rtmedia.AgentLLMPath(ctx, ws, spillRel)
	}
	hint := fmt.Sprintf("\n\n[Output truncated. Full output: %s]", display)
	if len(hint) >= maxViewBytes {
		return pruneToolText(full, maxViewBytes)
	}
	bodyBudget := maxViewBytes - len(hint)
	body := full
	if len(body) > bodyBudget {
		body = body[:bodyBudget]
	}
	return body + hint
}

func toolSpillRelPath(ws cw.Service, sessionID agentkit.SessionID, callID agentkit.ToolCallID) string {
	sess := rctx.SanitizeDirSegment(string(sessionID))
	call := rctx.SanitizeDirSegment(string(callID))
	if call == "" || call == "_" {
		call = "call"
	}
	workRel, _ := cw.WorkLayout(ws)
	return cw.JoinWork(workRel, "tool-spill/"+sess+"/"+call+".txt")
}

// SpillPathAbs resolves a stored spill_path audit entry to an absolute path.
func SpillPathAbs(ctx context.Context, ws cw.Service, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("empty spill path")
	}
	if ws == nil {
		return "", fmt.Errorf("workspace required")
	}
	return ws.Resolve(ctx, rel)
}
