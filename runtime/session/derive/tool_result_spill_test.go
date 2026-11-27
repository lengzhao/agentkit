package derive_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestPrepareToolResultForStorageSpillsLargeOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	ctx := rctx.WithWorkspaceService(context.Background(), ws)

	full := strings.Repeat("line\n", 5000)
	stored, err := derive.PrepareToolResultForStorage(ctx, agentkit.SessionID("sess-a"), agentkit.ToolResult{
		ID:      "call-1",
		Name:    "bash",
		Content: full,
	}, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Content) >= len(full) {
		t.Fatalf("expected truncated view, content len=%d", len(stored.Content))
	}
	spill := stored.Audit[derive.AuditSpillPath]
	if spill == "" {
		t.Fatal("missing spill_path audit")
	}
	if !strings.Contains(stored.Content, `read_tool_output(tool_call_id="call-1", offset=0)`) {
		t.Fatalf("content = %q", stored.Content)
	}
	if len(stored.Content) > 200 {
		t.Fatalf("view with hint should fit maxViewBytes=200, len=%d", len(stored.Content))
	}
	abs, err := derive.SpillPathAbs(ctx, ws, spill)
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != full {
		t.Fatalf("spill bytes = %d want %d", len(onDisk), len(full))
	}
}

func TestPrepareToolResultForStorageWithoutWorkspaceTruncates(t *testing.T) {
	t.Parallel()

	full := strings.Repeat("x", 9000)
	stored, err := derive.PrepareToolResultForStorage(context.Background(), agentkit.SessionID("s"), agentkit.ToolResult{
		ID:      "c",
		Name:    "bash",
		Content: full,
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Audit != nil && stored.Audit[derive.AuditSpillPath] != "" {
		t.Fatal("unexpected spill without workspace")
	}
	if !strings.HasSuffix(stored.Content, "\n...[truncated]") {
		t.Fatalf("content not truncated: len=%d", len(stored.Content))
	}
}

func TestPrepareToolResultForStorageSpillWriteFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	ctx := rctx.WithWorkspaceService(context.Background(), ws)

	rel := "work/tool-spill"
	abs, err := ws.Resolve(ctx, rel)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	// A regular file blocks the spill directory independently of identity encoding.
	if err := os.WriteFile(abs, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}

	stored, err := derive.PrepareToolResultForStorage(ctx, agentkit.SessionID("sess-b"), agentkit.ToolResult{
		ID:      "call-2",
		Name:    "bash",
		Content: strings.Repeat("x", 500),
	}, 100)
	if err != nil {
		t.Fatalf("spill failure should fall back to truncation: %v", err)
	}
	if stored.Audit[derive.AuditSpillPath] != "" {
		t.Fatal("failed spill was incorrectly recorded as readable")
	}
	if len(stored.Content) >= 500 {
		t.Fatalf("expected truncated content, got len=%d", len(stored.Content))
	}
	if !strings.HasSuffix(stored.Content, "\n...[truncated]") {
		t.Fatalf("content not truncated: len=%d", len(stored.Content))
	}
}

func TestReadToolOutputPagesAndIsolation(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	ctx := rctx.WithWorkspaceService(context.Background(), ws)
	full := strings.Repeat("中文\n\"", 2500)
	stored, err := derive.PrepareToolResultForStorage(ctx, "sess:a", agentkit.ToolResult{ID: "call:a", Content: full}, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored.Content, "read_tool_output") || !strings.Contains(stored.Content, "call:a") {
		t.Fatalf("no usable continuation: %s", stored.Content)
	}
	var got strings.Builder
	offset := int64(0)
	for {
		page, err := derive.ReadToolOutput(ctx, "sess:a", "call:a", offset)
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.ValidString(page.Text) {
			t.Fatal("invalid UTF-8")
		}
		raw, _ := json.Marshal(page)
		if len(raw) > 8192 {
			t.Fatalf("oversized page: %d", len(raw))
		}
		got.WriteString(page.Text)
		if page.NextOffset == nil {
			break
		}
		if *page.NextOffset <= offset {
			t.Fatal("no pagination progress")
		}
		offset = *page.NextOffset
	}
	if got.String() != full {
		t.Fatal("content mismatch")
	}
	for _, tc := range []struct {
		session, call string
		offset        int64
	}{
		{"sess_a", "call:a", 0}, {"sess:a", "call_a", 0}, {"sess:a", "unknown", 0}, {"sess:a", "../call:a", 0}, {"sess:a", "call:a", -1}, {"sess:a", "call:a", 1}, {"sess:a", "call:a", int64(len(full) + 1)}, {"", "call:a", 0},
	} {
		if _, err := derive.ReadToolOutput(ctx, agentkit.SessionID(tc.session), agentkit.ToolCallID(tc.call), tc.offset); err == nil {
			t.Fatalf("accepted invalid read: %+v", tc)
		}
	}
}

func TestReadToolOutputRejectsSymlinks(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	ctx := rctx.WithWorkspaceService(context.Background(), ws)
	stored, err := derive.PrepareToolResultForStorage(ctx, "s", agentkit.ToolResult{ID: "c", Content: strings.Repeat("x", 9000)}, 8192)
	if err != nil {
		t.Fatal(err)
	}
	path, err := derive.SpillPathAbs(ctx, ws, stored.Audit[derive.AuditSpillPath])
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, path); err != nil {
		t.Fatal(err)
	}
	if _, err := derive.ReadToolOutput(ctx, "s", "c", 0); err == nil {
		t.Fatal("read symbolic link")
	}
}

func TestReadToolOutputConcurrentSessionDirectoryReplacement(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	ctx := rctx.WithWorkspaceService(context.Background(), ws)
	own := strings.Repeat("own", 4000)
	stored, err := derive.PrepareToolResultForStorage(ctx, "own-session", agentkit.ToolResult{ID: "call", Content: own}, 8192)
	if err != nil {
		t.Fatal(err)
	}
	path, err := derive.SpillPathAbs(ctx, ws, stored.Audit[derive.AuditSpillPath])
	if err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Dir(path)
	parked := sessionDir + "-parked"
	sibling := filepath.Join(filepath.Dir(sessionDir), "other-session")
	if err := os.Mkdir(sibling, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, filepath.Base(path)), []byte("OTHER SESSION SECRET"), 0600); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if os.Rename(sessionDir, parked) != nil {
				continue
			}
			_ = os.Symlink(sibling, sessionDir)
			runtime.Gosched()
			_ = os.Remove(sessionDir)
			_ = os.Rename(parked, sessionDir)
		}
	}()
	defer func() { close(stop); <-done }()
	for i := 0; i < 1000; i++ {
		page, err := derive.ReadToolOutput(ctx, "own-session", "call", 0)
		if err == nil && !strings.HasPrefix(own, page.Text) {
			t.Fatalf("read another session: %q", page.Text)
		}
	}
}
