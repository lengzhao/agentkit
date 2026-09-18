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
