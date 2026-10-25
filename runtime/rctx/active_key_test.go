package rctx_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestActiveSessionEntryKey(t *testing.T) {
	t.Parallel()

	delivery := rctx.BuildDeliverySessionID("slack", "D0AK8MAHW22", "", "U02LNUW8KV5")
	conv := rctx.BuildDeliverySessionID("chat-api", "default_channel", "conv_1", "")

	cases := []struct {
		name     string
		platform string
		delivery agentkit.SessionID
		scope    agentkit.SessionScope
		userID   string
		want     agentkit.SessionID
	}{
		{
			name:     "slack channel scope",
			platform: "slack",
			delivery: delivery,
			scope:    agentkit.SessionScopeChannel,
			userID:   "U02LNUW8KV5",
			want:     "slack:D0AK8MAHW22",
		},
		{
			name:     "slack user scope",
			platform: "slack",
			delivery: delivery,
			scope:    agentkit.SessionScopeUser,
			userID:   "U02LNUW8KV5",
			want:     delivery,
		},
		{
			name:     "chat-api keeps conversation delivery",
			platform: "chat-api",
			delivery: conv,
			scope:    agentkit.SessionScopeChannel,
			want:     conv,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rctx.ActiveSessionEntryKey(tc.platform, tc.delivery, tc.scope, tc.userID)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// A channel-scoped bind key must stay at channel granularity no matter the
// configured conversation scope, so a bind set in one thread resolves from
// every thread of the same chat.
func TestChannelEntryKeyIgnoresThreadScope(t *testing.T) {
	t.Parallel()

	// Same chat, two different threads, thread-scoped conversation policy.
	deliveryA := rctx.BuildDeliverySessionID("lark", "oc_d95", "om_topicA", "ou_user")
	deliveryB := rctx.BuildDeliverySessionID("lark", "oc_d95", "om_topicB", "ou_user")
	threadPolicy := rctx.DefaultRoutePolicy(agentkit.SessionScopeThread)

	var keys []agentkit.SessionID
	for _, d := range []agentkit.SessionID{deliveryA, deliveryB} {
		route := rctx.SessionRoute("lark", string(d))
		keys = append(keys, rctx.ChannelEntryKey(route, threadPolicy, "ou_user"))
	}
	if keys[0] != keys[1] {
		t.Fatalf("channel key fragmented by thread: %q != %q", keys[0], keys[1])
	}
	if want := agentkit.SessionID("lark:oc_d95"); keys[0] != want {
		t.Fatalf("channel key = %q, want %q", keys[0], want)
	}
}

// ActiveEntryKey (the /new active-session key) must still follow the thread
// scope; only the channel bind key is pinned to the channel.
func TestChannelEntryKeyDoesNotChangeActiveEntryKey(t *testing.T) {
	t.Parallel()

	deliveryA := rctx.BuildDeliverySessionID("lark", "oc_d95", "om_topicA", "ou_user")
	deliveryB := rctx.BuildDeliverySessionID("lark", "oc_d95", "om_topicB", "ou_user")
	threadPolicy := rctx.DefaultRoutePolicy(agentkit.SessionScopeThread)

	activeA := rctx.ActiveEntryKey(rctx.SessionRoute("lark", string(deliveryA)), threadPolicy, "ou_user")
	activeB := rctx.ActiveEntryKey(rctx.SessionRoute("lark", string(deliveryB)), threadPolicy, "ou_user")
	if activeA == activeB {
		t.Fatalf("active entry key must stay thread-scoped, both = %q", activeA)
	}
}

// Opaque-delivery platforms (ActiveEntryDelivery) keep their raw delivery id.
func TestChannelEntryKeyOpaquePlatformPassthrough(t *testing.T) {
	t.Parallel()

	delivery := rctx.BuildDeliverySessionID("chat-api", "default_channel", "conv_1", "")
	route := rctx.SessionRoute("chat-api", string(delivery))
	policy := rctx.RoutePolicyForPlatform("chat-api", rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel))
	if got := rctx.ChannelEntryKey(route, policy, ""); got != delivery {
		t.Fatalf("opaque channel key = %q, want %q", got, delivery)
	}
}

// The user level is taken from the actor id (ctx), not only the delivery :u:
// segment: a delivery that omits :u: still yields a user key. Opaque-delivery
// platforms have no sub-scoping, so their finer levels stay empty.
func TestBindKeysUserLevelFromActor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		platform string
		delivery agentkit.SessionID
		policy   rctx.RoutePolicy
		userID   string
		wantUser agentkit.SessionID
	}{
		{
			name:     "delivery carries :u:",
			platform: "lark",
			delivery: "lark:oc_d95:t:om_1:u:ou_actor",
			policy:   rctx.RoutePolicyForPlatform("lark", rctx.DefaultRoutePolicy(agentkit.SessionScopeThread)),
			userID:   "ou_actor",
			wantUser: "lark:oc_d95:u:ou_actor",
		},
		{
			// Actor id is authoritative even when delivery omits :u:; every IM
			// platform supplies it, so user is never empty on real traffic.
			name:     "delivery omits :u:, actor supplies it",
			platform: "lark",
			delivery: "lark:oc_d95:t:om_1",
			policy:   rctx.RoutePolicyForPlatform("lark", rctx.DefaultRoutePolicy(agentkit.SessionScopeThread)),
			userID:   "ou_actor",
			wantUser: "lark:oc_d95:u:ou_actor",
		},
		{
			// Opaque-delivery platform: no sub-scoping, finer levels empty.
			name:     "opaque delivery has no user level",
			platform: "chat-api",
			delivery: "chat-api:default_channel:t:conv_1",
			policy:   rctx.RoutePolicyForPlatform("chat-api", rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel)),
			userID:   "u_actor",
			wantUser: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keys := rctx.BindKeysForRoute(rctx.SessionRoute(tc.platform, string(tc.delivery)), tc.policy, tc.userID, "")
			if keys.User != tc.wantUser {
				t.Fatalf("user key = %q, want %q", keys.User, tc.wantUser)
			}
		})
	}
}

// End-to-end via ctx: the actor id on the envelope populates the user level even
// when the route delivery has no :u: segment.
func TestBindKeysFromContextUsesActor(t *testing.T) {
	t.Parallel()

	delivery := rctx.BuildDeliverySessionID("lark", "oc_d95", "om_1", "")
	env := rctx.ResolveEnvelope(rctx.WithDeliveryRoute(agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: agentkit.RouteRef{Instance: "lark"},
		Actor: agentkit.ActorRef{UserID: "ou_actor"}}}, rctx.SessionRoute("lark", string(delivery))), rctx.RoutePolicyForPlatform("lark", rctx.DefaultRoutePolicy(agentkit.SessionScopeThread)))
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), env)

	keys := rctx.BindKeysFromContext(ctx)
	if keys.User != "lark:oc_d95:u:ou_actor" {
		t.Fatalf("user = %q (actor id not applied)", keys.User)
	}
	if keys.Channel != "lark:oc_d95" {
		t.Fatalf("channel = %q", keys.Channel)
	}
	if keys.Thread != "lark:oc_d95:t:om_1" {
		t.Fatalf("thread = %q", keys.Thread)
	}
}

// BuildSessionRoute backfills structured fields from the delivery id, so a
// caller that supplies only a delivery id (WithDeliverySession and the headless
// transports) still yields a fully-populated route target — and therefore every
// bind level resolves without the consumer re-parsing the id string.
func TestBuildSessionRouteBackfillsTargetFromDelivery(t *testing.T) {
	t.Parallel()

	route := rctx.WithInboundRoute(agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: agentkit.RouteRef{Instance: "lark"}}}, agentkit.SessionRouteInput{
		Platform:   "lark",
		DeliveryID: "lark:oc_d95:t:om_1:u:ou_actor",
	}).Envelope.Route

	target, ok := rctx.DecodeSessionRoute(route)
	if !ok {
		t.Fatal("route target missing")
	}
	if target.ChannelID != "oc_d95" || target.ThreadID != "om_1" || target.ScopeUserID != "ou_actor" {
		t.Fatalf("target not backfilled: ch=%q th=%q u=%q", target.ChannelID, target.ThreadID, target.ScopeUserID)
	}

	policy := rctx.RoutePolicyForPlatform("lark", rctx.DefaultRoutePolicy(agentkit.SessionScopeThread))
	keys := rctx.BindKeysForRoute(route, policy, "ou_actor", "")
	if keys.Channel != "lark:oc_d95" || keys.Thread != "lark:oc_d95:t:om_1" || keys.User != "lark:oc_d95:u:ou_actor" {
		t.Fatalf("keys ch=%q th=%q u=%q", keys.Channel, keys.Thread, keys.User)
	}
}

// Explicit structured fields still win; backfill never overwrites them.
func TestBuildSessionRouteKeepsExplicitTarget(t *testing.T) {
	t.Parallel()

	route := rctx.BuildSessionRoute(agentkit.SessionRouteInput{
		Platform:    "lark",
		DeliveryID:  "lark:oc_d95:t:om_1:u:ou_actor",
		ChannelID:   "oc_other",
		ThreadID:    "om_2",
		ScopeUserID: "ou_other",
	})
	target, _ := rctx.DecodeSessionRoute(route)
	if target.ChannelID != "oc_other" || target.ThreadID != "om_2" || target.ScopeUserID != "ou_other" {
		t.Fatalf("explicit fields clobbered: %+v", target)
	}
}

// DecodeSessionRoute backfills structured components from the delivery id on
// every path, so a bare-string target or a legacy flat-JSON target (persisted
// before structured fields existed) still yields channel/thread/user.
func TestDecodeSessionRouteBackfillsStructuredFields(t *testing.T) {
	t.Parallel()

	const delivery = "lark:oc_d95:t:om_1:u:ou_actor"

	bare := agentkit.RouteRef{
		Platform: "lark",
		Kind:     agentkit.RouteKindSession,
		Target:   json.RawMessage(`"` + delivery + `"`),
	}
	legacy := agentkit.RouteRef{
		Platform: "lark",
		Kind:     agentkit.RouteKindSession,
		Target:   json.RawMessage(`{"deliveryId":"` + delivery + `"}`),
	}
	for name, route := range map[string]agentkit.RouteRef{"bare-string": bare, "legacy-flat": legacy} {
		target, ok := rctx.DecodeSessionRoute(route)
		if !ok {
			t.Fatalf("%s: decode failed", name)
		}
		if target.ChannelID != "oc_d95" || target.ThreadID != "om_1" || target.ScopeUserID != "ou_actor" {
			t.Fatalf("%s: not backfilled: ch=%q th=%q u=%q", name, target.ChannelID, target.ThreadID, target.ScopeUserID)
		}
	}
}

// Legacy flat-JSON route survives UnmarshalJSON then decodes with full fields,
// and its bind keys resolve — the persisted-job path needs no re-parsing.
func TestLegacyFlatRouteYieldsFullBindKeys(t *testing.T) {
	t.Parallel()

	var route agentkit.RouteRef
	if err := json.Unmarshal([]byte(`{"platform":"lark","kind":"session","sessionId":"lark:oc_d95:t:om_1","userId":"ou_actor"}`), &route); err != nil {
		t.Fatal(err)
	}
	policy := rctx.RoutePolicyForPlatform("lark", rctx.DefaultRoutePolicy(agentkit.SessionScopeThread))
	keys := rctx.BindKeysForRoute(route, policy, "ou_actor", "")
	if keys.Channel != "lark:oc_d95" || keys.Thread != "lark:oc_d95:t:om_1" || keys.User != "lark:oc_d95:u:ou_actor" {
		t.Fatalf("keys ch=%q th=%q u=%q", keys.Channel, keys.Thread, keys.User)
	}
}
