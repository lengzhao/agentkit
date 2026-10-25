package rctx_test

import (
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
