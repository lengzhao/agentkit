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
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: agentkit.SessionRoute("slack", "slack:C001"), Conversation: "slack:C001", Workspace: "slack:C001"})
	ctx = session.ContextWithDeliveryRoute(ctx, "slack", agentkit.SessionID("slack:C001:t:111.0:u:U456"))
	ctx = session.WithAgentID(ctx, agentkit.AgentID("coder"))
	ctx = func() context.Context { env := session.EnvelopeFromContext(ctx); env.Route = agentkit.SessionRoute("slack", "delivery"); return session.ApplyEnvelopeToContext(ctx, env) }()
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

	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: agentkit.SessionRoute("slack", "slack:C001"), Conversation: "slack:C001", Workspace: "slack:C001"})
	ctx = session.ContextWithDeliveryRoute(ctx, "slack", agentkit.SessionID("slack:C001:t:111.0:u:U456"))
	ctx = session.WithAgentID(ctx, agentkit.AgentID("coder"))

	if _, err := tool.Call(ctx, []byte(`{"text":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	if len(platform.sent) != 1 {
		t.Fatalf("sent=%d want 1", len(platform.sent))
	}
	if session.OutboundRouteID(platform.sent[0]) != "slack:C001:t:111.0:u:U456" {
		t.Fatalf("route=%q", session.OutboundRouteID(platform.sent[0]))
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
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: agentkit.SessionRoute("slack", "slack:C001"), Conversation: "slack:C001", Workspace: "slack:C001"})
	ctx = session.ContextWithDeliveryRoute(ctx, "slack", inbox)
	ctx = session.WithAgentID(ctx, agentkit.AgentID("coder"))

	if _, err := tool.Call(ctx, []byte(`{"text":"hi","userId":"U222"}`)); err != nil {
		t.Fatal(err)
	}
	if len(platform.sent) != 1 {
		t.Fatalf("sent=%d want 1", len(platform.sent))
	}
	want := session.BuildDeliverySessionID("slack", "C001", "111.0", "U222")
	if session.OutboundRouteID(platform.sent[0]) != want {
		t.Fatalf("route=%q want %q", session.OutboundRouteID(platform.sent[0]), want)
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

	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: agentkit.SessionRoute("slack", "slack:C1"), Conversation: "slack:C1", Workspace: "slack:C1"})
	ctx = session.ContextWithDeliveryRoute(ctx, "slack", agentkit.SessionID("slack:C1"))
	ctx = session.WithAgentID(ctx, agentkit.AgentID("coder"))

	if _, err := tool.Call(ctx, []byte(`{"path":"work/report.pdf"}`)); err != nil {
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

	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "cli:default", Workspace: "cli:default"})
	ctx = session.ContextWithDeliveryRoute(ctx, "cli", agentkit.SessionID("cli:default"))
	if _, err := tool.Call(ctx, []byte(`{"text":"see attached","path":"work/report.pdf"}`)); err != nil {
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

	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: agentkit.SessionRoute("slack", "slack:C1"), Conversation: "slack:C1", Workspace: "slack:C1"})
	ctx = session.ContextWithDeliveryRoute(ctx, "slack", agentkit.SessionID("slack:C1"))
	ctx = session.WithAgentID(ctx, agentkit.AgentID("coder"))

	if _, err := tool.Call(ctx, []byte(`{"path":"work/upload/hello.go"}`)); err != nil {
		t.Fatal(err)
	}
	if len(platform.sent) != 1 {
		t.Fatalf("sent=%d want 1", len(platform.sent))
	}
}
