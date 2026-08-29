package session_test

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestActiveSessionEntryKey(t *testing.T) {
	t.Parallel()

	delivery := session.BuildDeliverySessionID("slack", "D0AK8MAHW22", "", "U02LNUW8KV5")
	conv := session.BuildDeliverySessionID("chat-api", "default_channel", "conv_1", "")

	cases := []struct {
		name     string
		platform string
		delivery agentkit.SessionID
		scope    session.SessionScope
		userID   string
		want     agentkit.SessionID
	}{
		{
			name:     "slack channel scope",
			platform: "slack",
			delivery: delivery,
			scope:    session.ScopeChannel,
			userID:   "U02LNUW8KV5",
			want:     "slack:D0AK8MAHW22",
		},
		{
			name:     "slack user scope",
			platform: "slack",
			delivery: delivery,
			scope:    session.ScopeUser,
			userID:   "U02LNUW8KV5",
			want:     delivery,
		},
		{
			name:     "chat-api keeps conversation delivery",
			platform: "chat-api",
			delivery: conv,
			scope:    session.ScopeChannel,
			want:     conv,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := session.ActiveSessionEntryKey(tc.platform, tc.delivery, tc.scope, tc.userID)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
