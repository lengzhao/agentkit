package acpremote

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
	rtfilesystem "github.com/lengzhao/agentkit/runtime/filesystem"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func bindTestFS(t *testing.T, dir string) filesystem.Service {
	t.Helper()
	fs, err := rtfilesystem.New(rtfilesystem.Config{Root: "."}, rtfilesystem.Deps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestACPSessionBindRoundTrip(t *testing.T) {
	dir := t.TempDir()
	fs := bindTestFS(t, dir)
	ctx := context.Background()
	path, err := acpSessionBindPath(".", agentkit.SessionID("chat:thread-1"), agentkit.AgentID("claude"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "acp-session.claude.json" {
		t.Fatalf("path = %q", path)
	}
	bind := acpSessionBind{
		AgentID:      "claude",
		ACPSessionID: acp.SessionId("acp_sess_123"),
		Cwd:          "/workspace/project",
	}
	if err := saveACPSessionBind(ctx, fs, path, bind); err != nil {
		t.Fatal(err)
	}
	got, ok, err := loadACPSessionBind(ctx, fs, path)
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if got.ACPSessionID != bind.ACPSessionID || got.Cwd != bind.Cwd {
		t.Fatalf("bind mismatch: %+v", got)
	}
}

func TestACPSessionBindPathPerAgent(t *testing.T) {
	dir := t.TempDir()
	fs := bindTestFS(t, dir)
	ctx := context.Background()
	sessionID := agentkit.SessionID("chat:thread-1")
	claudePath, err := acpSessionBindPath(".", sessionID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	cursorPath, err := acpSessionBindPath(".", sessionID, "cursor")
	if err != nil {
		t.Fatal(err)
	}
	if claudePath == cursorPath {
		t.Fatalf("expected distinct bind paths: %q", claudePath)
	}
	if err := saveACPSessionBind(ctx, fs, claudePath, acpSessionBind{
		AgentID: "claude", ACPSessionID: "acp_claude", Cwd: dir,
	}); err != nil {
		t.Fatal(err)
	}
	if err := saveACPSessionBind(ctx, fs, cursorPath, acpSessionBind{
		AgentID: "cursor", ACPSessionID: "acp_cursor", Cwd: dir,
	}); err != nil {
		t.Fatal(err)
	}
	claudeBind, ok, err := loadACPSessionBind(ctx, fs, claudePath)
	if err != nil || !ok || claudeBind.ACPSessionID != "acp_claude" {
		t.Fatalf("claude bind: %+v ok=%v err=%v", claudeBind, ok, err)
	}
	cursorBind, ok, err := loadACPSessionBind(ctx, fs, cursorPath)
	if err != nil || !ok || cursorBind.ACPSessionID != "acp_cursor" {
		t.Fatalf("cursor bind: %+v ok=%v err=%v", cursorBind, ok, err)
	}
}

func TestACPSessionBindPathUsesSessionWorkDir(t *testing.T) {
	dir := t.TempDir()
	fs := bindTestFS(t, dir)
	path, err := acpSessionBindPath(".", agentkit.SessionID("a/b"), agentkit.AgentID("claude"))
	if err != nil {
		t.Fatal(err)
	}
	if err := saveACPSessionBind(context.Background(), fs, path, acpSessionBind{
		AgentID:      "claude",
		ACPSessionID: "sid",
		Cwd:          dir,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
		t.Fatalf("expected bind file under session work dir: %v", err)
	}
}

func TestPriorMessages(t *testing.T) {
	messages := []agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "first"}}},
		{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "reply"}}},
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "current"}}},
	}
	history := priorMessages(messages)
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2", len(history))
	}
	if history[1].Content[0].Text != "reply" {
		t.Fatalf("unexpected history: %+v", history)
	}
}
