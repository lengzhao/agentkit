package derive

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/lengzhao/agentkit"
	cw "github.com/lengzhao/agentkit/cap/workspace"
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
	out.Content = toolResultViewWithSpillHint(result.Content, result.ID, maxViewBytes)
	if out.Audit == nil {
		out.Audit = make(map[string]string, 1)
	}
	out.Audit[AuditSpillPath] = rel
	return out, nil
}

func toolResultViewWithSpillHint(full string, callID agentkit.ToolCallID, maxViewBytes int) string {
	hint := fmt.Sprintf("\n\n[Output truncated. Read complete output with read_tool_output(tool_call_id=%q, offset=0); follow next_offset until absent.]", callID)
	if len(hint) >= maxViewBytes {
		return pruneToolText(full, maxViewBytes)
	}
	bodyBudget := maxViewBytes - len(hint)
	body := full
	if len(body) > bodyBudget {
		body = body[:bodyBudget]
		for !utf8.ValidString(body) && len(body) > 0 {
			body = body[:len(body)-1]
		}
	}
	return body + hint
}

func toolSpillRelPath(ws cw.Service, sessionID agentkit.SessionID, callID agentkit.ToolCallID) string {
	// Hash opaque identities without lossy sanitization: e.g. sess:a and sess_a
	// must never share an output directory, even with a shared workspace.
	sess := fmt.Sprintf("%x", sha256.Sum256([]byte(sessionID)))
	call := fmt.Sprintf("%x", sha256.Sum256([]byte(callID)))
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

// ToolOutputPage is a bounded UTF-8 slice of an existing spilled tool result.
// Offsets count bytes in the original output and never split a code point.
type ToolOutputPage struct {
	Text       string `json:"text"`
	NextOffset *int64 `json:"next_offset,omitempty"`
}

// ReadToolOutput reads only a call's spill in the supplied current session.
// The tool adapter obtains the session from rctx; callers cannot select paths.
func ReadToolOutput(ctx context.Context, sessionID agentkit.SessionID, callID agentkit.ToolCallID, offset int64) (ToolOutputPage, error) {
	if sessionID == "" || callID == "" || offset < 0 {
		return ToolOutputPage{}, fmt.Errorf("session, tool_call_id and a non-negative offset required")
	}
	ws := rctx.WorkspaceServiceFromContext(ctx)
	if ws == nil {
		return ToolOutputPage{}, fmt.Errorf("tool output unavailable: workspace required")
	}
	rel := toolSpillRelPath(ws, sessionID, callID)
	abs, err := ws.Resolve(ctx, rel)
	if err != nil {
		return ToolOutputPage{}, err
	}
	// Reject links along the spill path, including links into another session.
	// Workspace ancestors may themselves be platform-managed symbolic links.
	root, err := ws.Resolve(ctx, ".")
	if err != nil {
		return ToolOutputPage{}, err
	}
	sub, err := filepath.Rel(root, abs)
	if err != nil || sub == ".." || strings.HasPrefix(sub, ".."+string(filepath.Separator)) {
		return ToolOutputPage{}, fmt.Errorf("output outside workspace")
	}
	// Pin every directory before descending. OpenRoot alone follows links
	// inside its root: compare the opened inode with Lstat so a concurrent
	// replacement cannot redirect this lookup into a different session.
	dir, err := os.OpenRoot(root)
	if err != nil {
		return ToolOutputPage{}, fmt.Errorf("tool output unavailable")
	}
	defer func() { _ = dir.Close() }()
	parts := strings.Split(sub, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		info, err := dir.Lstat(part)
		if err != nil || !info.IsDir() {
			return ToolOutputPage{}, fmt.Errorf("tool output directory unavailable")
		}
		child, err := dir.OpenRoot(part)
		if err != nil {
			return ToolOutputPage{}, fmt.Errorf("tool output directory unavailable")
		}
		opened, err := child.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			child.Close()
			return ToolOutputPage{}, fmt.Errorf("tool output directory changed")
		}
		dir.Close()
		dir = child
	}
	name := parts[len(parts)-1]
	before, err := dir.Lstat(name)
	if err != nil || !before.Mode().IsRegular() {
		return ToolOutputPage{}, fmt.Errorf("tool output unavailable")
	}
	// dir is now the pinned session directory; Open cannot follow a link to a
	// sibling session even if the file changes after Lstat.
	f, err := dir.Open(name)
	if err != nil {
		return ToolOutputPage{}, fmt.Errorf("tool output unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ToolOutputPage{}, err
	}
	if !os.SameFile(before, info) || !info.Mode().IsRegular() || offset > info.Size() {
		return ToolOutputPage{}, fmt.Errorf("invalid output offset")
	}
	// 1024 raw bytes remain below the 8192 byte view limit even when JSON escapes
	// every byte, and leave room for tool wrappers and pagination metadata.
	buf := make([]byte, 1028)
	n, err := f.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return ToolOutputPage{}, err
	}
	buf = buf[:n]
	if len(buf) > 0 && !utf8.RuneStart(buf[0]) {
		return ToolOutputPage{}, fmt.Errorf("offset must be at a UTF-8 boundary")
	}
	end := len(buf)
	if end > 1024 {
		end = 1024
		for end > 0 && !utf8.RuneStart(buf[end]) {
			end--
		}
	}
	page := ToolOutputPage{Text: string(buf[:end])}
	if !utf8.ValidString(page.Text) {
		return ToolOutputPage{}, fmt.Errorf("tool output is not valid UTF-8")
	}
	if next := offset + int64(end); next < info.Size() {
		page.NextOffset = &next
	}
	return page, nil
}
