package send

import (
	"context"
	"errors"
	"testing"

	"github.com/lengzhao/agentkit"
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

func withSendCtx(ctx context.Context, sessionID, deliveryID agentkit.SessionID) context.Context {
	ctx = context.WithValue(ctx, agentkit.KeySessionID, sessionID)
	ctx = context.WithValue(ctx, agentkit.KeyDeliverySessionID, deliveryID)
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentkit.AgentID("assistant"))
	return ctx
}

func TestParseSlashArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args    string
		text    string
		session string
		user    string
		wantErr bool
	}{
		{args: "slack:C001 ping", session: "slack:C001", text: "ping"},
		{args: "D0AK8MAHW22 123", session: "D0AK8MAHW22", text: "123"},
		{args: "@U222 hi there", user: "U222", text: "hi there"},
		{args: "hello inbox", wantErr: true},
		{args: "hello", wantErr: true},
		{args: "not-a-target hello", wantErr: true},
		{args: "", wantErr: true},
		{args: "slack:C001", wantErr: true},
		{args: "@U222", wantErr: true},
	}
	for _, tc := range cases {
		input, err := ParseSlashArgs(tc.args)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("ParseSlashArgs(%q) expected error", tc.args)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseSlashArgs(%q): %v", tc.args, err)
		}
		if input.Text != tc.text || input.SessionID != tc.session || input.UserID != tc.user {
			t.Fatalf("ParseSlashArgs(%q) = %#v", tc.args, input)
		}
	}
}

func TestSendSlashCommandRequiresTarget(t *testing.T) {
	t.Parallel()

	platform := &recordingPlatform{}
	tool, err := NewSend(SendConfig{}, SendDeps{Platform: platform})
	if err != nil {
		t.Fatal(err)
	}
	bundle := tool.(*sendBundle)
	ctx := withSendCtx(t.Context(), "slack:C001", "slack:C001")
	_, err = bundle.Commands()[0].CommandExec(ctx, "hello inbox")
	if err == nil {
		t.Fatal("expected error for bare message")
	}
	if len(platform.sent) != 0 {
		t.Fatalf("sent = %#v", platform.sent)
	}
}

func TestSendSlashCommandTargetSession(t *testing.T) {
	t.Parallel()

	platform := &recordingPlatform{}
	tool, err := NewSend(SendConfig{}, SendDeps{Platform: platform})
	if err != nil {
		t.Fatal(err)
	}
	bundle := tool.(*sendBundle)
	ctx := withSendCtx(t.Context(), "slack:C001", "slack:C001")
	ctx = context.WithValue(ctx, agentkit.KeyPlatformID, "slack")
	out, err := bundle.Commands()[0].CommandExec(ctx, "slack:C002 remote ping")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("reply = %q", out)
	}
	if len(platform.sent) != 1 || platform.sent[0].SessionID != "slack:C002" {
		t.Fatalf("sent = %#v", platform.sent)
	}
	if platform.sent[0].PlatformID != "slack" {
		t.Fatalf("platformID = %q, want slack", platform.sent[0].PlatformID)
	}
}

func TestSendSlashCommandRoutesToTargetSessionPlatform(t *testing.T) {
	t.Parallel()

	platform := &recordingPlatform{}
	tool, err := NewSend(SendConfig{}, SendDeps{Platform: platform})
	if err != nil {
		t.Fatal(err)
	}
	bundle := tool.(*sendBundle)
	target := "chat-api:nex-channel:t:conv_test"
	ctx := withSendCtx(t.Context(), "slack:C001", "slack:C001")
	ctx = context.WithValue(ctx, agentkit.KeyPlatformID, "slack")
	_, err = bundle.Commands()[0].CommandExec(ctx, target+" hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(platform.sent) != 1 {
		t.Fatalf("sent = %#v", platform.sent)
	}
	if platform.sent[0].SessionID != agentkit.SessionID(target) {
		t.Fatalf("session = %q", platform.sent[0].SessionID)
	}
	if platform.sent[0].PlatformID != "chat-api" {
		t.Fatalf("platformID = %q, want chat-api", platform.sent[0].PlatformID)
	}
}

func TestSendSlashCommandTargetUser(t *testing.T) {
	t.Parallel()

	platform := &recordingPlatform{}
	tool, err := NewSend(SendConfig{}, SendDeps{Platform: platform})
	if err != nil {
		t.Fatal(err)
	}
	bundle := tool.(*sendBundle)
	ctx := withSendCtx(t.Context(), "slack:C001", "slack:C001:t:1:u:U1")
	_, err = bundle.Commands()[0].CommandExec(ctx, "@U222 hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(platform.sent) != 1 || platform.sent[0].UserID != "U222" {
		t.Fatalf("sent = %#v", platform.sent)
	}
}

func TestSendSlashCommandRequiresPlatformID(t *testing.T) {
	t.Parallel()

	platform := &recordingPlatform{}
	tool, err := NewSend(SendConfig{}, SendDeps{Platform: platform})
	if err != nil {
		t.Fatal(err)
	}
	bundle := tool.(*sendBundle)
	ctx := withSendCtx(t.Context(), "notvalid", "notvalid")
	_, err = bundle.Commands()[0].CommandExec(ctx, "@U222 hello")
	if !errors.Is(err, agentkit.ErrOutboundPlatformRequired) {
		t.Fatalf("err = %v, want ErrOutboundPlatformRequired", err)
	}
	if len(platform.sent) != 0 {
		t.Fatalf("sent = %#v", platform.sent)
	}
}
