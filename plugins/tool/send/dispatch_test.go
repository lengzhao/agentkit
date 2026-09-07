package send

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
	rtdelivery "github.com/lengzhao/agentkit/runtime/delivery"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestNormalizeSessionIDBareChat(t *testing.T) {
	t.Parallel()

	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: agentkit.SessionRoute("feishu", "delivery")})
	if got := rtdelivery.NormalizeSessionID(ctx, "oc_a1b2c3d4e5"); got != "feishu:oc_a1b2c3d4e5" {
		t.Fatalf("feishu: got %q", got)
	}
	ctx = session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: agentkit.SessionRoute("slack", "delivery")})
	if got := rtdelivery.NormalizeSessionID(ctx, "D0AK8MAHW22"); got != "slack:D0AK8MAHW22" {
		t.Fatalf("slack: got %q", got)
	}
	if got := rtdelivery.NormalizeSessionID(ctx, "feishu:oc_x"); got != "feishu:oc_x" {
		t.Fatalf("explicit: got %q", got)
	}
	if got := rtdelivery.NormalizeSessionID(context.Background(), "oc_x"); got != "oc_x" {
		t.Fatalf("no platform: got %q", got)
	}
}

func TestResolveRouteSlackBareChannel(t *testing.T) {
	t.Parallel()

	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: agentkit.SessionRoute("slack", "delivery")})
	ctx = session.ContextWithDeliveryRoute(ctx, "slack", agentkit.SessionID("slack:C001:u:U1"))
	route, err := rtdelivery.ResolveRoute(ctx, capsdelivery.RouteInput{SessionID: "D0AK8MAHW22"})
	if err != nil {
		t.Fatal(err)
	}
	if route.SessionID != "slack:D0AK8MAHW22" {
		t.Fatalf("sessionID = %q", route.SessionID)
	}
	if route.PlatformID != "slack" {
		t.Fatalf("platformID = %q", route.PlatformID)
	}
}

func TestResolveRouteFeishuBareChat(t *testing.T) {
	t.Parallel()

	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: agentkit.SessionRoute("feishu", "delivery")})
	ctx = session.ContextWithDeliveryRoute(ctx, "feishu", agentkit.SessionID("feishu:oc_src"))
	route, err := rtdelivery.ResolveRoute(ctx, capsdelivery.RouteInput{SessionID: "oc_dst"})
	if err != nil {
		t.Fatal(err)
	}
	if route.SessionID != "feishu:oc_dst" {
		t.Fatalf("sessionID = %q", route.SessionID)
	}
	if route.PlatformID != "feishu" {
		t.Fatalf("platformID = %q", route.PlatformID)
	}
}

func TestSendSlashCommandBareFeishuChat(t *testing.T) {
	t.Parallel()

	platform := &recordingPlatform{}
	tool, err := NewSend(SendConfig{}, SendDeps{Sender: platform})
	if err != nil {
		t.Fatal(err)
	}
	bundle := tool.(*sendBundle)
	ctx := withSendCtx(t.Context(), "feishu", "feishu:oc_src", "feishu:oc_src")
	ctx = func() context.Context {
		env := session.EnvelopeFromContext(ctx)
		env.Route = agentkit.SessionRoute("feishu", "delivery")
		return session.ApplyEnvelopeToContext(ctx, env)
	}()
	_, err = bundle.Commands()[0].CommandExec(ctx, "oc_dst hello\nworld")
	if err != nil {
		t.Fatal(err)
	}
	if len(platform.sent) != 1 {
		t.Fatalf("sent = %#v", platform.sent)
	}
	if rtdelivery.OutboundRouteID(platform.sent[0]) != "feishu:oc_dst" {
		t.Fatalf("route = %q", rtdelivery.OutboundRouteID(platform.sent[0]))
	}
	if platform.sent[0].PlatformID != "feishu" {
		t.Fatalf("platformID = %q", platform.sent[0].PlatformID)
	}
	var msg agentkit.ModelMessage
	if err := json.Unmarshal(platform.sent[0].Data, &msg); err != nil {
		t.Fatal(err)
	}
	if len(msg.Content) != 1 || msg.Content[0].Text != "hello\nworld" {
		t.Fatalf("content = %#v", msg.Content)
	}
}
