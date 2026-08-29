package send_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/tool/send"
	"github.com/lengzhao/agentkit/runtime/session"
)

type recordingPlatform struct {
	sent []agentkit.OutboundEvent
}

func (p *recordingPlatform) Receive(context.Context) (agentkit.MessageEvent, error) {
	return agentkit.MessageEvent{}, nil
}

func (p *recordingPlatform) Send(_ context.Context, event agentkit.OutboundEvent) error {
	p.sent = append(p.sent, event)
	return nil
}

func TestSendUsesEmitForCurrentInbox(t *testing.T) {
	t.Parallel()

	platform := &recordingPlatform{}
	tool, err := send.NewSend(send.SendConfig{}, send.SendDeps{Platform: platform})
	if err != nil {
		t.Fatal(err)
	}

	var emitted []agentkit.OutboundEvent
	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("slack:C001"))
	ctx = context.WithValue(ctx, agentkit.KeyDeliverySessionID, agentkit.SessionID("slack:C001:t:111.0:u:U456"))
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentkit.AgentID("coder"))
	ctx = context.WithValue(ctx, agentkit.KeyPlatformID, "slack")
	ctx = context.WithValue(ctx, agentkit.KeyOutboundEmit, agentkit.OutboundEmit(func(_ context.Context, event agentkit.OutboundEvent) error {
		emitted = append(emitted, event)
		return nil
	}))

	if _, err := tool.Call(ctx, []byte(`{"text":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	if len(emitted) != 1 {
		t.Fatalf("emitted=%d want 1", len(emitted))
	}
	if len(platform.sent) != 0 {
		t.Fatal("platform should not be called when emit is available")
	}
}

func TestSendUsesInboxDeliverySession(t *testing.T) {
	t.Parallel()

	platform := &recordingPlatform{}
	tool, err := send.NewSend(send.SendConfig{}, send.SendDeps{Platform: platform})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("slack:C001"))
	ctx = context.WithValue(ctx, agentkit.KeyDeliverySessionID, agentkit.SessionID("slack:C001:t:111.0:u:U456"))
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentkit.AgentID("coder"))

	if _, err := tool.Call(ctx, []byte(`{"text":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	if len(platform.sent) != 1 {
		t.Fatalf("sent=%d want 1", len(platform.sent))
	}
	if platform.sent[0].SessionID != "slack:C001:t:111.0:u:U456" {
		t.Fatalf("session=%q", platform.sent[0].SessionID)
	}
}

func TestSendUserIDTarget(t *testing.T) {
	t.Parallel()

	platform := &recordingPlatform{}
	tool, err := send.NewSend(send.SendConfig{}, send.SendDeps{Platform: platform})
	if err != nil {
		t.Fatal(err)
	}

	inbox := session.BuildDeliverySessionID("slack", "C001", "111.0", "U111")
	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("slack:C001"))
	ctx = context.WithValue(ctx, agentkit.KeyDeliverySessionID, inbox)
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentkit.AgentID("coder"))

	if _, err := tool.Call(ctx, []byte(`{"text":"hi","userId":"U222"}`)); err != nil {
		t.Fatal(err)
	}
	if len(platform.sent) != 1 {
		t.Fatalf("sent=%d want 1", len(platform.sent))
	}
	want := session.BuildDeliverySessionID("slack", "C001", "111.0", "U222")
	if platform.sent[0].SessionID != want {
		t.Fatalf("session=%q want %q", platform.sent[0].SessionID, want)
	}
	if platform.sent[0].UserID != "U222" {
		t.Fatalf("user=%q", platform.sent[0].UserID)
	}
}

func TestSendFilePath(t *testing.T) {
	t.Parallel()

	platform := &recordingPlatform{}
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "report.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := workspace.Static(root)
	tool, err := send.NewSend(send.SendConfig{}, send.SendDeps{Platform: platform, Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("slack:C1"))
	ctx = context.WithValue(ctx, agentkit.KeyDeliverySessionID, agentkit.SessionID("slack:C1"))
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentkit.AgentID("coder"))

	if _, err := tool.Call(ctx, []byte(`{"path":"report.pdf"}`)); err != nil {
		t.Fatal(err)
	}
	if len(platform.sent) != 1 {
		t.Fatalf("sent=%d want 1", len(platform.sent))
	}
	var msg agentkit.ModelMessage
	if err := json.Unmarshal(platform.sent[0].Data, &msg); err != nil {
		t.Fatal(err)
	}
	if len(msg.Content) != 1 || msg.Content[0].Type != "document" {
		t.Fatalf("content=%#v", msg.Content)
	}
}

func TestSendTextAndPath(t *testing.T) {
	t.Parallel()

	platform := &recordingPlatform{}
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "report.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := workspace.Static(root)
	tool, err := send.NewSend(send.SendConfig{}, send.SendDeps{Platform: platform, Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("cli:default"))
	ctx = context.WithValue(ctx, agentkit.KeyDeliverySessionID, agentkit.SessionID("cli:default"))
	if _, err := tool.Call(ctx, []byte(`{"text":"see attached","path":"report.pdf"}`)); err != nil {
		t.Fatal(err)
	}
	if len(platform.sent) != 1 {
		t.Fatalf("sent=%d want 1", len(platform.sent))
	}
	var msg agentkit.ModelMessage
	if err := json.Unmarshal(platform.sent[0].Data, &msg); err != nil {
		t.Fatal(err)
	}
	if len(msg.Content) != 2 || msg.Content[0].Text != "see attached" || msg.Content[1].Type != "document" {
		t.Fatalf("content=%#v", msg.Content)
	}
}

func TestSendInboundAttachmentPath(t *testing.T) {
	t.Parallel()

	platform := &recordingPlatform{}
	root := t.TempDir()
	attachDir := filepath.Join(root, "work", "upload")
	if err := os.MkdirAll(attachDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attachDir, "hello.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := workspace.Static(root)
	tool, err := send.NewSend(send.SendConfig{}, send.SendDeps{Platform: platform, Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("slack:C1"))
	ctx = context.WithValue(ctx, agentkit.KeyDeliverySessionID, agentkit.SessionID("slack:C1"))
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentkit.AgentID("coder"))

	if _, err := tool.Call(ctx, []byte(`{"path":"upload/hello.go"}`)); err != nil {
		t.Fatal(err)
	}
	if len(platform.sent) != 1 {
		t.Fatalf("sent=%d want 1", len(platform.sent))
	}
}
