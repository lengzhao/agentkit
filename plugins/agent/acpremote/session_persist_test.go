package acpremote

import (
	"os"
	"path/filepath"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
)

func TestACPSessionBindRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path, err := acpSessionBindPath(dir, agentkit.SessionID("chat:thread-1"), agentkit.AgentID("claude"))
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
	if err := saveACPSessionBind(path, bind); err != nil {
		t.Fatal(err)
	}
	got, ok, err := loadACPSessionBind(path)
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if got.ACPSessionID != bind.ACPSessionID || got.Cwd != bind.Cwd {
		t.Fatalf("bind mismatch: %+v", got)
	}
}

func TestACPSessionBindPathPerAgent(t *testing.T) {
	dir := t.TempDir()
	sessionID := agentkit.SessionID("chat:thread-1")
	claudePath, err := acpSessionBindPath(dir, sessionID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	cursorPath, err := acpSessionBindPath(dir, sessionID, "cursor")
	if err != nil {
		t.Fatal(err)
	}
	if claudePath == cursorPath {
		t.Fatalf("expected distinct bind paths: %q", claudePath)
	}
	if err := saveACPSessionBind(claudePath, acpSessionBind{
		AgentID: "claude", ACPSessionID: "acp_claude", Cwd: dir,
	}); err != nil {
		t.Fatal(err)
	}
	if err := saveACPSessionBind(cursorPath, acpSessionBind{
		AgentID: "cursor", ACPSessionID: "acp_cursor", Cwd: dir,
	}); err != nil {
		t.Fatal(err)
	}
	claudeBind, ok, err := loadACPSessionBind(claudePath)
	if err != nil || !ok || claudeBind.ACPSessionID != "acp_claude" {
		t.Fatalf("claude bind: %+v ok=%v err=%v", claudeBind, ok, err)
	}
	cursorBind, ok, err := loadACPSessionBind(cursorPath)
	if err != nil || !ok || cursorBind.ACPSessionID != "acp_cursor" {
		t.Fatalf("cursor bind: %+v ok=%v err=%v", cursorBind, ok, err)
	}
}

func TestACPSessionBindPathUsesSessionWorkDir(t *testing.T) {
	dir := t.TempDir()
	path, err := acpSessionBindPath(dir, agentkit.SessionID("a/b"), agentkit.AgentID("claude"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveACPSessionBind(path, acpSessionBind{
		AgentID:      "claude",
		ACPSessionID: "sid",
		Cwd:          dir,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
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
