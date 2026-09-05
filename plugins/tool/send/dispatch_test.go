package send

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestNormalizeSlashSessionIDSlackChannel(t *testing.T) {
	t.Parallel()

	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: agentkit.SessionRoute("slack", "delivery")})
	got := common.NormalizeDeliverySessionID(ctx, "D0AK8MAHW22")
	want := agentkit.SessionID("slack:D0AK8MAHW22")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveRouteSlackBareChannel(t *testing.T) {
	t.Parallel()

	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: agentkit.SessionRoute("slack", "delivery")})
	ctx = session.ContextWithDeliveryRoute(ctx, "slack", agentkit.SessionID("slack:C001:u:U1"))
	route, err := resolveRoute(ctx, SendInput{SessionID: "D0AK8MAHW22", Text: "123"})
	if err != nil {
		t.Fatal(err)
	}
	if route.sessionID != "slack:D0AK8MAHW22" {
		t.Fatalf("sessionID = %q", route.sessionID)
	}
	if route.platformID != "slack" {
		t.Fatalf("platformID = %q", route.platformID)
	}
}
