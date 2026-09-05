package send

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
	rtdelivery "github.com/lengzhao/agentkit/runtime/delivery"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestNormalizeSlashSessionIDSlackChannel(t *testing.T) {
	t.Parallel()

	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: agentkit.SessionRoute("slack", "delivery")})
	got := rtdelivery.NormalizeSessionID(ctx, "D0AK8MAHW22")
	want := agentkit.SessionID("slack:D0AK8MAHW22")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
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
